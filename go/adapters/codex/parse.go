package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	stem "hop.top/stem"
)

// Option configures Parse.
type Option func(*parser)

// WithNativePath records path as the envelope's native store file
// (MetaNativePath metadata) and enables recovering the session id
// from the rollout filename when no session_meta record survives.
func WithNativePath(path string) Option {
	return func(p *parser) { p.nativePath = path }
}

// ParseFile parses one rollout file into a crtx Envelope. The
// absolute file path is recorded as MetaNativePath.
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
	return Parse(f, WithNativePath(abs))
}

// Parse streams rollout JSONL from r into a crtx Envelope. Content
// problems are absorbed into the Report (see package doc); the only
// parse-level failures are reader errors and inputs with no session
// identity (ErrNoSession).
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

// record is the top-level shape of every rollout line. Unknown
// sibling fields are ignored by encoding/json, which is the drift
// posture we want at this layer.
type record struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// sessionMeta is the payload of a session_meta record, across
// vintages. Old stores carry instructions (string|null); new stores
// carry base_instructions (object{text}|string|null) plus linkage
// fields.
type sessionMeta struct {
	ID               string          `json:"id"`
	SessionID        string          `json:"session_id"`
	Timestamp        string          `json:"timestamp"`
	Cwd              string          `json:"cwd"`
	CliVersion       string          `json:"cli_version"`
	Instructions     string          `json:"instructions"`
	BaseInstructions json.RawMessage `json:"base_instructions"`
	ForkedFromID     string          `json:"forked_from_id"`
	ParentThreadID   string          `json:"parent_thread_id"`
	Git              *gitInfo        `json:"git"`
}

type gitInfo struct {
	CommitHash string `json:"commit_hash"`
	Branch     string `json:"branch"`
}

type parser struct {
	rep        *Report
	nativePath string

	meta      *sessionMeta
	createdAt time.Time
	updatedAt time.Time
	lastTS    time.Time
	turns     []stem.Turn
	openCalls map[string]struct{}
}

func (p *parser) line(raw []byte) {
	p.rep.Lines++

	var rec record
	if err := json.Unmarshal(raw, &rec); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	ts := parseTime(rec.Timestamp)
	if !ts.IsZero() {
		p.lastTS = ts
		if p.createdAt.IsZero() {
			p.createdAt = ts
		}
		if ts.After(p.updatedAt) {
			p.updatedAt = ts
		}
	}

	switch rec.Type {
	case "session_meta":
		p.sessionMeta(rec)
	case "response_item":
		p.responseItem(rec, ts)
	case "event_msg":
		p.rep.skip(SkipEventRecord)
	case "turn_context":
		p.rep.skip(SkipTurnContext)
	case "world_state":
		p.rep.skip(SkipWorldState)
	case "compacted":
		p.rep.skip(SkipCompacted)
	default:
		p.rep.skip(SkipUnknownRecord)
	}
}

func (p *parser) sessionMeta(rec record) {
	if p.meta != nil {
		// Fork files carry the copied parent's session_meta (and some
		// vintages repeat their own); the first record is the file's
		// identity, the rest are history.
		p.rep.skip(SkipDuplicateMeta)
		return
	}
	var m sessionMeta
	if err := json.Unmarshal(rec.Payload, &m); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	p.meta = &m
	if ts := parseTime(m.Timestamp); !ts.IsZero() {
		p.createdAt = ts
	}
}

func (p *parser) responseItem(rec record, ts time.Time) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(rec.Payload, &probe); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	switch probe.Type {
	case "message":
		p.message(rec.Payload, ts)
	case "reasoning":
		p.reasoning(rec.Payload, ts)
	case "function_call":
		p.toolCall(rec.Payload, ts, false)
	case "custom_tool_call":
		p.toolCall(rec.Payload, ts, true)
	case "function_call_output", "custom_tool_call_output":
		p.toolResult(rec.Payload, ts)
	case "web_search_call":
		// No call_id in the native record; nothing crtx-valid to emit.
		p.rep.skip(SkipWebSearchCall)
	default:
		p.rep.skip(SkipUnknownItem)
	}
}

func (p *parser) message(payload json.RawMessage, ts time.Time) {
	var msg struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL string `json:"image_url"`
			URL      string `json:"url"`
		} `json:"content"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	role, ok := mapRole(msg.Role)
	if !ok {
		p.rep.skip(SkipUnknownRole)
		return
	}
	var parts []stem.ContentPart
	for _, c := range msg.Content {
		switch c.Type {
		case "input_text", "output_text", "text":
			parts = append(parts, stem.TextPart(c.Text))
		case "input_image", "output_image", "image":
			url := c.ImageURL
			if url == "" {
				url = c.URL
			}
			part, ok := imagePart(url)
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
	p.emit(role, ts, parts...)
}

func (p *parser) reasoning(payload json.RawMessage, ts time.Time) {
	var rs struct {
		Summary []struct {
			Text string `json:"text"`
		} `json:"summary"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(payload, &rs); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	var parts []stem.ContentPart
	for _, s := range rs.Summary {
		if s.Text != "" {
			parts = append(parts, stem.ContentPart{Type: stem.PartTypeThinking, Text: s.Text})
		}
	}
	for _, c := range rs.Content {
		if c.Text != "" {
			parts = append(parts, stem.ContentPart{Type: stem.PartTypeThinking, Text: c.Text})
		}
	}
	if len(parts) == 0 {
		// Encrypted-only reasoning: nothing representable.
		p.rep.skip(SkipEmptyReasoning)
		return
	}
	p.emit(stem.RoleAssistant, ts, parts...)
}

func (p *parser) toolCall(payload json.RawMessage, ts time.Time, custom bool) {
	var tc struct {
		Name      string `json:"name"`
		CallID    string `json:"call_id"`
		Arguments string `json:"arguments"`
		Input     string `json:"input"`
	}
	if err := json.Unmarshal(payload, &tc); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	if tc.CallID == "" || tc.Name == "" {
		p.rep.skip(SkipInvalidToolCall)
		return
	}
	var input json.RawMessage
	switch {
	case custom:
		// custom_tool_call input is an opaque string by contract.
		input = mustJSONString(tc.Input)
	case tc.Arguments != "" && json.Valid([]byte(tc.Arguments)):
		input = json.RawMessage(tc.Arguments)
	default:
		input = mustJSONString(tc.Arguments)
	}
	p.openCalls[tc.CallID] = struct{}{}
	p.emit(stem.RoleAssistant, ts, stem.ContentPart{
		Type:   stem.PartTypeToolCall,
		CallID: tc.CallID,
		Name:   tc.Name,
		Input:  input,
	})
}

func (p *parser) toolResult(payload json.RawMessage, ts time.Time) {
	var tr struct {
		CallID string          `json:"call_id"`
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(payload, &tr); err != nil {
		p.rep.skip(SkipMalformedLine)
		return
	}
	if _, open := p.openCalls[tr.CallID]; !open {
		// Preceding call was dropped (or never recorded); emitting the
		// result would break crtx tool linkage.
		p.rep.skip(SkipOrphanToolResult)
		return
	}
	delete(p.openCalls, tr.CallID)
	output := tr.Output
	if len(output) == 0 {
		output = json.RawMessage(`null`)
	}
	p.emit(stem.RoleTool, ts, stem.ContentPart{
		Type:   stem.PartTypeToolResult,
		CallID: tr.CallID,
		Output: output,
	})
}

func (p *parser) emit(role stem.Role, ts time.Time, parts ...stem.ContentPart) {
	if ts.IsZero() {
		ts = p.lastTS
	}
	if ts.IsZero() {
		ts = p.createdAt
	}
	p.turns = append(p.turns, stem.Turn{
		ID:        fmt.Sprintf("t_%06d", len(p.turns)+1),
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

	s := &stem.Session{
		CrtxVersion: stem.CrtxVersion,
		ID:          id,
		CreatedAt:   p.createdAt,
		UpdatedAt:   p.updatedAt,
		Source:      stem.Source{Kind: SourceKind, Version: version},
		Turns:       p.turns,
		Metadata:    p.metadata(id),
	}
	if parent := p.parentID(id); parent != "" {
		fp := 0
		s.ParentID = parent
		s.ForkPoint = &fp
	}
	return s, nil
}

// identity resolves the session id and source version. Precedence
// for the id: session_meta.id, session_meta.session_id, rollout
// filename UUID. The id is always the native store's identifier
// verbatim, never re-minted.
func (p *parser) identity() (id, version string) {
	version = "unknown"
	if p.meta != nil {
		if p.meta.CliVersion != "" {
			version = p.meta.CliVersion
		}
		if p.meta.ID != "" {
			return p.meta.ID, version
		}
		if p.meta.SessionID != "" {
			return p.meta.SessionID, version
		}
	}
	return filenameUUID(p.nativePath), version
}

// parentID resolves fork linkage per the precedence documented in
// the package doc. id is this session's own id; a session_id equal
// to it carries no linkage.
func (p *parser) parentID(id string) string {
	if p.meta == nil {
		return ""
	}
	if p.meta.ForkedFromID != "" {
		return p.meta.ForkedFromID
	}
	if p.meta.ParentThreadID != "" {
		return p.meta.ParentThreadID
	}
	if p.meta.SessionID != "" && p.meta.SessionID != id {
		return p.meta.SessionID
	}
	return ""
}

func (p *parser) metadata(id string) map[string]any {
	md := make(map[string]any)
	if p.nativePath != "" {
		md[MetaNativePath] = p.nativePath
	}
	if p.meta != nil {
		if p.meta.Cwd != "" {
			md[MetaCwd] = p.meta.Cwd
		}
		if p.meta.Git != nil {
			if p.meta.Git.Branch != "" {
				md[MetaGitBranch] = p.meta.Git.Branch
			}
			if p.meta.Git.CommitHash != "" {
				md[MetaGitCommit] = p.meta.Git.CommitHash
			}
		}
		if inst := p.instructions(); inst != "" {
			md[MetaInstructions] = inst
		}
	}
	if len(md) == 0 {
		return nil
	}
	return md
}

// instructions extracts the session's system instructions across
// vintages: base_instructions as {text} object or bare string, then
// the legacy instructions string.
func (p *parser) instructions() string {
	if len(p.meta.BaseInstructions) > 0 && !bytes.Equal(p.meta.BaseInstructions, []byte("null")) {
		var obj struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(p.meta.BaseInstructions, &obj); err == nil && obj.Text != "" {
			return obj.Text
		}
		var s string
		if err := json.Unmarshal(p.meta.BaseInstructions, &s); err == nil && s != "" {
			return s
		}
	}
	return p.meta.Instructions
}

func mapRole(r string) (stem.Role, bool) {
	switch r {
	case "user":
		return stem.RoleUser, true
	case "assistant":
		return stem.RoleAssistant, true
	case "system":
		return stem.RoleSystem, true
	case "developer":
		return stem.RoleDeveloper, true
	case "tool":
		return stem.RoleTool, true
	}
	return "", false
}

var dataURLRe = regexp.MustCompile(`^data:([a-z]+/[a-zA-Z0-9.+-]+);base64,(.*)$`)

// imagePart maps a Codex image URL to a crtx image part. data: URLs
// carry mime and payload inline; anything else rides as a URL with
// an octet-stream mime (crtx requires a mime, the native store does
// not record one).
func imagePart(url string) (stem.ContentPart, bool) {
	if url == "" {
		return stem.ContentPart{}, false
	}
	if m := dataURLRe.FindStringSubmatch(url); m != nil {
		return stem.ContentPart{Type: stem.PartTypeImage, Mime: m[1], Data: m[2]}, true
	}
	return stem.ContentPart{Type: stem.PartTypeImage, Mime: "application/octet-stream", URL: url}, true
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// filenameUUID recovers the session id from a rollout filename
// (rollout-<timestamp>-<uuid>.jsonl). Returns "" when path does not
// carry one.
func filenameUUID(path string) string {
	if path == "" {
		return ""
	}
	name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if len(name) < 36 {
		return ""
	}
	cand := name[len(name)-36:]
	if !uuidRe.MatchString(cand) {
		return ""
	}
	return cand
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
