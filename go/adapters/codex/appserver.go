package codex

import (
	"encoding/json"
	"hop.top/stem/adapters"
	"strings"
	"time"

	stem "hop.top/stem"
)

// Skip reasons specific to app-server JSON-RPC lines.
const (
	// SkipDelta counts streaming delta notifications; the completed
	// item carries the full content.
	SkipDelta = "delta"
	// SkipNotification counts RPC messages that map to no turn:
	// responses, requests, lifecycle notifications, item/started.
	SkipNotification = "notification"
)

// NewLineReader returns a reader ready for Feed.
func NewLineReader(opts ...Option) *LineReader {
	p := &LineReader{
		rep:       &Report{},
		openCalls: map[string]struct{}{},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithSessionID seeds the session id used when no session_meta or
// thread/started message supplies one. Native identity wins.
func WithSessionID(id string) Option {
	return func(p *LineReader) { p.seedID = id }
}

// Account implements adapters.LineReader.
func (p *LineReader) Account() adapters.Accounting { return p.rep }

// rpc handles one app-server JSON-RPC message. Only thread/started
// (identity) and item/completed (content) contribute; the rest is
// accounted.
func (p *LineReader) rpc(rec record) {
	switch {
	case rec.Method == "thread/started":
		p.threadStarted(rec.Params)
	case rec.Method == "item/completed":
		p.itemCompleted(rec.Params)
	case isDelta(rec.Method):
		p.rep.skip(SkipDelta)
	default:
		p.rep.skip(SkipNotification)
	}
}

func isDelta(method string) bool {
	return strings.HasPrefix(method, "item/") &&
		(strings.HasSuffix(method, "Delta") || strings.HasSuffix(method, "/delta"))
}

func (p *LineReader) threadStarted(params json.RawMessage) {
	var m struct {
		Thread struct {
			ID             string   `json:"id"`
			SessionID      string   `json:"sessionId"`
			CliVersion     string   `json:"cliVersion"`
			Cwd            string   `json:"cwd"`
			CreatedAt      int64    `json:"createdAt"`
			ForkedFromID   string   `json:"forkedFromId"`
			ParentThreadID string   `json:"parentThreadId"`
			GitInfo        *gitWire `json:"gitInfo"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(params, &m); err != nil || m.Thread.ID == "" {
		p.rep.skip(SkipMalformedLine)
		return
	}
	if p.meta != nil {
		p.rep.skip(SkipDuplicateMeta)
		return
	}
	t := m.Thread
	p.meta = &sessionMeta{
		ID:             t.ID,
		SessionID:      t.SessionID,
		CliVersion:     t.CliVersion,
		Cwd:            t.Cwd,
		ForkedFromID:   t.ForkedFromID,
		ParentThreadID: t.ParentThreadID,
	}
	if g := t.GitInfo; g != nil {
		p.meta.Git = &gitInfo{CommitHash: g.SHA, Branch: g.Branch}
	}
	if t.CreatedAt > 0 {
		ts := time.Unix(t.CreatedAt, 0).UTC()
		p.createdAt = ts
		p.observe(ts)
	}
}

type gitWire struct {
	SHA    string `json:"sha"`
	Branch string `json:"branch"`
}

func (p *LineReader) itemCompleted(params json.RawMessage) {
	var m struct {
		CompletedAtMs *int64          `json:"completedAtMs"`
		Item          json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(params, &m); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	var ts time.Time
	if m.CompletedAtMs != nil {
		ts = time.UnixMilli(*m.CompletedAtMs).UTC()
		p.observe(ts)
	}
	var probe struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(m.Item, &probe); err != nil || probe.ID == "" {
		p.rep.skip(SkipMalformedLine)
		return
	}
	switch probe.Type {
	case "userMessage":
		p.asUserMessage(probe.ID, m.Item, ts)
	case "agentMessage", "plan":
		p.asText(probe.ID, m.Item, ts)
	case "reasoning":
		p.asReasoning(probe.ID, m.Item, ts)
	case "commandExecution":
		p.asCommand(probe.ID, m.Item, ts)
	case "fileChange":
		p.asFileChange(probe.ID, m.Item, ts)
	case "mcpToolCall":
		p.asMCP(probe.ID, m.Item, ts)
	case "dynamicToolCall":
		p.asDynamic(probe.ID, m.Item, ts)
	default:
		p.rep.skip(SkipUnknownItem)
	}
}

func (p *LineReader) asUserMessage(id string, raw json.RawMessage, ts time.Time) {
	var it struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			URL  string `json:"url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	var parts []stem.ContentPart
	for _, c := range it.Content {
		switch c.Type {
		case "text":
			parts = append(parts, stem.TextPart(c.Text))
		case "image":
			part, ok := imagePart(c.URL)
			if !ok {
				p.rep.skip(SkipUnknownPart)
				continue
			}
			parts = append(parts, part)
		default:
			p.rep.skip(SkipUnknownPart)
		}
	}
	if len(parts) == 0 {
		p.rep.skip(SkipEmptyMessage)
		return
	}
	p.append(id, stem.RoleUser, ts, parts...)
}

func (p *LineReader) asText(id string, raw json.RawMessage, ts time.Time) {
	var it struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	if it.Text == "" {
		p.rep.skip(SkipEmptyMessage)
		return
	}
	p.append(id, stem.RoleAssistant, ts, stem.TextPart(it.Text))
}

func (p *LineReader) asReasoning(id string, raw json.RawMessage, ts time.Time) {
	var it struct {
		Summary []string `json:"summary"`
		Content []string `json:"content"`
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	var parts []stem.ContentPart
	for _, s := range append(it.Summary, it.Content...) {
		if s != "" {
			parts = append(parts, stem.ContentPart{Type: stem.PartTypeThinking, Text: s})
		}
	}
	if len(parts) == 0 {
		p.rep.skip(SkipEmptyReasoning)
		return
	}
	p.append(id, stem.RoleAssistant, ts, parts...)
}

// toolPair emits the assistant tool_call turn (id = item id) and the
// tool turn carrying its result (id = item id + "/result"). The
// item id doubles as the call id; app-server items are completed
// atomically, so the pair is always emitted together.
func (p *LineReader) toolPair(id, name string, input, output any, isErr bool, ts time.Time) {
	p.append(id, stem.RoleAssistant, ts, stem.ContentPart{
		Type:   stem.PartTypeToolCall,
		CallID: id,
		Name:   name,
		Input:  mustJSON(input),
	})
	p.append(id+"/result", stem.RoleTool, ts, stem.ContentPart{
		Type:    stem.PartTypeToolResult,
		CallID:  id,
		Output:  mustJSON(output),
		IsError: isErr,
	})
}

func failedStatus(s string) bool { return s == "failed" || s == "declined" }

func (p *LineReader) asCommand(id string, raw json.RawMessage, ts time.Time) {
	var it struct {
		Command  string  `json:"command"`
		Cwd      string  `json:"cwd"`
		Output   *string `json:"aggregatedOutput"`
		ExitCode *int    `json:"exitCode"`
		Status   string  `json:"status"`
	}
	if err := json.Unmarshal(raw, &it); err != nil || it.Command == "" {
		p.rep.skip(SkipInvalidToolCall)
		return
	}
	isErr := failedStatus(it.Status) || (it.ExitCode != nil && *it.ExitCode != 0)
	p.toolPair(id, "commandExecution",
		map[string]any{"command": it.Command, "cwd": it.Cwd},
		map[string]any{"output": it.Output, "exit_code": it.ExitCode, "status": it.Status},
		isErr, ts)
}

func (p *LineReader) asFileChange(id string, raw json.RawMessage, ts time.Time) {
	var it struct {
		Changes json.RawMessage `json:"changes"`
		Status  string          `json:"status"`
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		p.rep.skip(SkipInvalidToolCall)
		return
	}
	p.toolPair(id, "fileChange",
		map[string]any{"changes": it.Changes},
		map[string]any{"status": it.Status},
		failedStatus(it.Status), ts)
}

func (p *LineReader) asMCP(id string, raw json.RawMessage, ts time.Time) {
	var it struct {
		Server    string          `json:"server"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		Result    json.RawMessage `json:"result"`
		Error     json.RawMessage `json:"error"`
		Status    string          `json:"status"`
	}
	if err := json.Unmarshal(raw, &it); err != nil || it.Tool == "" {
		p.rep.skip(SkipInvalidToolCall)
		return
	}
	p.toolPair(id, it.Tool, rawOrNull(it.Arguments),
		map[string]any{"server": it.Server, "result": rawOrNull(it.Result), "error": rawOrNull(it.Error)},
		failedStatus(it.Status), ts)
}

func (p *LineReader) asDynamic(id string, raw json.RawMessage, ts time.Time) {
	var it struct {
		Tool         string          `json:"tool"`
		Arguments    json.RawMessage `json:"arguments"`
		ContentItems json.RawMessage `json:"contentItems"`
		Success      *bool           `json:"success"`
		Status       string          `json:"status"`
	}
	if err := json.Unmarshal(raw, &it); err != nil || it.Tool == "" {
		p.rep.skip(SkipInvalidToolCall)
		return
	}
	isErr := failedStatus(it.Status) || (it.Success != nil && !*it.Success)
	p.toolPair(id, it.Tool, rawOrNull(it.Arguments),
		map[string]any{"content_items": rawOrNull(it.ContentItems), "success": it.Success},
		isErr, ts)
}

func rawOrNull(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return json.RawMessage(`null`)
	}
	return r
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}
