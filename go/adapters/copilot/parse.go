package copilot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	stem "hop.top/stem"
)

// Option configures Parse.
type Option func(*parser)

// WithNativePath records path as the envelope's native store file
// (MetaNativePath metadata).
func WithNativePath(path string) Option {
	return func(p *parser) { p.nativePath = path }
}

// WithStoreID supplies the session id the store keys this file under
// (directory name or flat-file stem), used when no session.start
// event survives.
func WithStoreID(id string) Option {
	return func(p *parser) { p.storeID = id }
}

// ParseFile parses one session event log into a crtx Envelope. The
// absolute file path is recorded as MetaNativePath; the store path
// supplies the fallback session id.
func ParseFile(path string) (*stem.Session, *Report, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	return Parse(f, WithNativePath(abs), WithStoreID(storeID(path)))
}

// storeID recovers the session id from a store path: the directory
// name for <id>/events.jsonl, the file stem for <id>.jsonl.
func storeID(path string) string {
	base := filepath.Base(path)
	if base == "events.jsonl" {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(base, ".jsonl")
}

// Parse streams session event JSONL from r into a crtx Envelope.
// Content problems are absorbed into the Report (see package doc);
// the only parse-level failures are reader errors and inputs with no
// session identity (ErrNoSession).
func Parse(r io.Reader, opts ...Option) (*stem.Session, *Report, error) {
	p := &parser{
		rep:       &Report{},
		openCalls: map[string]struct{}{},
	}
	for _, opt := range opts {
		opt(p)
	}

	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			p.line(bytes.TrimSpace(line))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, p.rep, err
		}
	}
	s, err := p.finish()
	return s, p.rep, err
}

// event is the top-level shape of every log line. parentId chains
// events within the file and is ignored; unknown sibling fields are
// ignored by encoding/json, which is the drift posture we want.
type event struct {
	ID        string          `json:"id"`
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
}

// sessionStart is the session.start payload subset this adapter
// reads.
type sessionStart struct {
	SessionID      string          `json:"sessionId"`
	CopilotVersion string          `json:"copilotVersion"`
	StartTime      string          `json:"startTime"`
	SelectedModel  string          `json:"selectedModel"`
	Context        *sessionContext `json:"context"`
}

// sessionContext is the working-directory/git context recorded at
// session start.
type sessionContext struct {
	Cwd        string `json:"cwd"`
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	HeadCommit string `json:"headCommit"`
}

type parser struct {
	rep        *Report
	nativePath string
	storeID    string

	start     *sessionStart
	createdAt time.Time
	updatedAt time.Time
	lastTS    time.Time
	turns     []stem.Turn
	openCalls map[string]struct{}
}

func (p *parser) line(raw []byte) {
	p.rep.Lines++

	var ev event
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Type == "" {
		p.rep.skip(SkipMalformedLine)
		return
	}
	ts := parseTime(ev.Timestamp)
	if !ts.IsZero() {
		p.lastTS = ts
		if p.createdAt.IsZero() {
			p.createdAt = ts
		}
		if ts.After(p.updatedAt) {
			p.updatedAt = ts
		}
	}

	switch ev.Type {
	case "session.start":
		p.sessionStart(ev)
	case "user.message":
		p.userMessage(ev, ts)
	case "assistant.message":
		p.assistantMessage(ev, ts)
	case "assistant.reasoning":
		p.reasoning(ev, ts)
	case "tool.user_requested":
		p.userToolCall(ev, ts)
	case "tool.execution_start":
		p.toolStart(ev, ts)
	case "tool.execution_complete":
		p.toolComplete(ev, ts)
	default:
		p.rep.skip(EventSkipPrefix + ev.Type)
	}
}

func (p *parser) sessionStart(ev event) {
	if p.start != nil {
		p.rep.skip(SkipDuplicateStart)
		return
	}
	var m sessionStart
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	p.start = &m
	if ts := parseTime(m.StartTime); !ts.IsZero() {
		p.createdAt = ts
	}
}

func (p *parser) userMessage(ev event, ts time.Time) {
	var m struct {
		Content     string            `json:"content"`
		Attachments []json.RawMessage `json:"attachments"`
	}
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	for range m.Attachments {
		p.rep.skip(SkipAttachment)
	}
	if m.Content == "" {
		p.rep.skip(SkipEmptyMessage)
		return
	}
	p.emit(stem.RoleUser, ts, ev.ID, stem.TextPart(m.Content))
}

func (p *parser) assistantMessage(ev event, ts time.Time) {
	var m struct {
		Content          string `json:"content"`
		ReasoningText    string `json:"reasoningText"`
		ReasoningOpaque  string `json:"reasoningOpaque"`
		EncryptedContent string `json:"encryptedContent"`
		ToolRequests     []struct {
			ToolCallID string          `json:"toolCallId"`
			Name       string          `json:"name"`
			Arguments  json.RawMessage `json:"arguments"`
		} `json:"toolRequests"`
	}
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	if (m.ReasoningOpaque != "" || m.EncryptedContent != "") && m.ReasoningText == "" {
		// Encrypted extended thinking: nothing representable.
		p.rep.skip(SkipOpaqueReasoning)
	}
	var parts []stem.ContentPart
	if m.ReasoningText != "" {
		parts = append(parts, stem.ContentPart{Type: stem.PartTypeThinking, Text: m.ReasoningText})
	}
	if m.Content != "" {
		parts = append(parts, stem.TextPart(m.Content))
	}
	for _, tr := range m.ToolRequests {
		if tr.ToolCallID == "" || tr.Name == "" {
			p.rep.skip(SkipInvalidToolCall)
			continue
		}
		p.openCalls[tr.ToolCallID] = struct{}{}
		parts = append(parts, stem.ContentPart{
			Type:   stem.PartTypeToolCall,
			CallID: tr.ToolCallID,
			Name:   tr.Name,
			Input:  tr.Arguments,
		})
	}
	if len(parts) == 0 {
		p.rep.skip(SkipEmptyMessage)
		return
	}
	p.emit(stem.RoleAssistant, ts, ev.ID, parts...)
}

func (p *parser) reasoning(ev event, ts time.Time) {
	var m struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	if m.Content == "" {
		p.rep.skip(SkipEmptyReasoning)
		return
	}
	p.emit(stem.RoleAssistant, ts, ev.ID, stem.ContentPart{Type: stem.PartTypeThinking, Text: m.Content})
}

// userToolCall maps tool.user_requested: the user invoked the tool
// directly, so the call rides on a user-role turn.
func (p *parser) userToolCall(ev event, ts time.Time) {
	var m struct {
		ToolCallID string          `json:"toolCallId"`
		ToolName   string          `json:"toolName"`
		Arguments  json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	if m.ToolCallID == "" || m.ToolName == "" {
		p.rep.skip(SkipInvalidToolCall)
		return
	}
	p.openCalls[m.ToolCallID] = struct{}{}
	p.emit(stem.RoleUser, ts, ev.ID, stem.ContentPart{
		Type:   stem.PartTypeToolCall,
		CallID: m.ToolCallID,
		Name:   m.ToolName,
		Input:  m.Arguments,
	})
}

// toolStart maps tool.execution_start. Normally the call is already
// open (assistant.message toolRequests or tool.user_requested) and
// the start is execution detail, skipped as a duplicate. A start for
// a never-opened call (sub-agent tool calls carry no
// assistant.message in the parent log) opens it as an assistant turn
// so the completion stays linked.
func (p *parser) toolStart(ev event, ts time.Time) {
	var m struct {
		ToolCallID string          `json:"toolCallId"`
		ToolName   string          `json:"toolName"`
		Arguments  json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	if m.ToolCallID == "" || m.ToolName == "" {
		p.rep.skip(SkipInvalidToolCall)
		return
	}
	if _, open := p.openCalls[m.ToolCallID]; open {
		p.rep.skip(SkipDuplicateToolCall)
		return
	}
	p.openCalls[m.ToolCallID] = struct{}{}
	p.emit(stem.RoleAssistant, ts, ev.ID, stem.ContentPart{
		Type:   stem.PartTypeToolCall,
		CallID: m.ToolCallID,
		Name:   m.ToolName,
		Input:  m.Arguments,
	})
}

func (p *parser) toolComplete(ev event, ts time.Time) {
	var m struct {
		ToolCallID string `json:"toolCallId"`
		Success    *bool  `json:"success"`
		Result     *struct {
			Content string `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(ev.Data, &m); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	if _, open := p.openCalls[m.ToolCallID]; !open {
		// The opening call was dropped (or never recorded); emitting
		// the result would break crtx tool linkage.
		p.rep.skip(SkipOrphanToolResult)
		return
	}
	delete(p.openCalls, m.ToolCallID)
	output := json.RawMessage(`null`)
	if m.Result != nil {
		output = mustJSONString(m.Result.Content)
	}
	p.emit(stem.RoleTool, ts, ev.ID, stem.ContentPart{
		Type:    stem.PartTypeToolResult,
		CallID:  m.ToolCallID,
		Output:  output,
		IsError: m.Success != nil && !*m.Success,
	})
}

func (p *parser) emit(role stem.Role, ts time.Time, eventID string, parts ...stem.ContentPart) {
	if ts.IsZero() {
		ts = p.lastTS
	}
	if ts.IsZero() {
		ts = p.createdAt
	}
	id := eventID
	if id == "" {
		id = fmt.Sprintf("t_%06d", len(p.turns)+1)
	}
	p.turns = append(p.turns, stem.Turn{
		ID:        id,
		Role:      role,
		CreatedAt: ts,
		Content:   parts,
	})
	p.rep.Turns++
}

func (p *parser) finish() (*stem.Session, error) {
	id, version := p.identity()
	if id == "" {
		return nil, ErrNoSession
	}
	if p.createdAt.IsZero() {
		return nil, fmt.Errorf("%w: no timestamps", ErrNoSession)
	}
	if p.updatedAt.IsZero() || p.updatedAt.Before(p.createdAt) {
		p.updatedAt = p.createdAt
	}

	return &stem.Session{
		CrtxVersion: stem.CrtxVersion,
		ID:          id,
		CreatedAt:   p.createdAt,
		UpdatedAt:   p.updatedAt,
		Source:      stem.Source{Kind: SourceKind, Version: version},
		Turns:       p.turns,
		Metadata:    p.metadata(),
	}, nil
}

// identity resolves the session id and source version. Precedence
// for the id: session.start sessionId, then the store path id. The
// id is always the native store's identifier verbatim, never
// re-minted.
func (p *parser) identity() (id, version string) {
	version = "unknown"
	if p.start != nil {
		if p.start.CopilotVersion != "" {
			version = p.start.CopilotVersion
		}
		if p.start.SessionID != "" {
			return p.start.SessionID, version
		}
	}
	return p.storeID, version
}

func (p *parser) metadata() map[string]any {
	md := make(map[string]any)
	if p.nativePath != "" {
		md[MetaNativePath] = p.nativePath
	}
	if p.start != nil {
		if p.start.SelectedModel != "" {
			md[MetaModel] = p.start.SelectedModel
		}
		if c := p.start.Context; c != nil {
			if c.Cwd != "" {
				md[MetaCwd] = c.Cwd
			}
			if c.Branch != "" {
				md[MetaGitBranch] = c.Branch
			}
			if c.HeadCommit != "" {
				md[MetaGitCommit] = c.HeadCommit
			}
			if c.Repository != "" {
				md[MetaRepository] = c.Repository
			}
		}
	}
	if len(md) == 0 {
		return nil
	}
	return md
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func mustJSONString(s string) json.RawMessage {
	b, err := json.Marshal(s)
	if err != nil {
		// Marshaling a string cannot fail; keep the compiler honest.
		return json.RawMessage(`""`)
	}
	return json.RawMessage(b)
}
