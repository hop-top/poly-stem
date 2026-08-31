package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	stem "hop.top/stem"
)

// nativeRecord is the tolerant superset of one store line across
// vintages. Unknown fields are ignored by encoding/json.
type nativeRecord struct {
	Type          string          `json:"type"`
	UUID          string          `json:"uuid"`
	SessionID     string          `json:"sessionId"`
	AgentID       string          `json:"agentId"`
	IsMeta        bool            `json:"isMeta"`
	Timestamp     string          `json:"timestamp"`
	CWD           string          `json:"cwd"`
	GitBranch     string          `json:"gitBranch"`
	Version       string          `json:"version"`
	Message       json.RawMessage `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	// Content is the queue-operation payload (a string); system
	// records reuse the key with other shapes, ignored here.
	Content json.RawMessage `json:"content"`
}

// nativeMessage is the API-shaped message payload; content is either
// a JSON string (legacy) or an array of content blocks.
type nativeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// nativeBlock is the tolerant superset of one content block.
type nativeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Signature string          `json:"signature"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    *nativeImage    `json:"source"`
}

type nativeImage struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	URL       string `json:"url"`
}

// ParseFile normalizes one transcript file into an envelope. The
// dispatch index, when given, resolves sidechain linkage (see the
// package doc); pass nil when parsing main sessions or when no
// parent context is available. The only error condition is I/O;
// content problems degrade into the Result's Account.
func ParseFile(path string, dispatch DispatchIndex) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("claudecode: %w", err)
	}
	defer f.Close()

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("claudecode: %w", err)
	}

	p := &parser{
		account:   Account{Path: path, SkippedRecords: map[string]int{}},
		refs:      DispatchIndex{},
		openCalls: map[string]struct{}{},
		sidechain: strings.HasPrefix(filepath.Base(path), agentFilePrefix),
	}

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			p.line(line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("claudecode: read %s: %w", path, err)
		}
	}

	return p.result(path, abs, dispatch), nil
}

// parser accumulates envelope state across one file's lines.
type parser struct {
	account   Account
	refs      DispatchIndex
	openCalls map[string]struct{}
	sidechain bool

	turns     []stem.Turn
	sessionID string
	agentID   string
	version   string
	cwd       string
	gitBranch string
}

func (p *parser) line(raw []byte) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return
	}
	var rec nativeRecord
	if err := json.Unmarshal([]byte(trimmed), &rec); err != nil {
		p.account.MalformedLines++
		return
	}

	// Identity and provenance: first non-empty value wins.
	if p.sessionID == "" {
		p.sessionID = rec.SessionID
	}
	if p.agentID == "" {
		p.agentID = rec.AgentID
	}
	if p.version == "" {
		p.version = rec.Version
	}
	if p.cwd == "" {
		p.cwd = rec.CWD
	}
	if p.gitBranch == "" {
		p.gitBranch = rec.GitBranch
	}

	switch rec.Type {
	case "user", "assistant":
		if rec.IsMeta {
			p.account.SkippedRecords[rec.Type+":meta"]++
			return
		}
		p.turnRecord(rec)
	case "queue-operation":
		// Not a turn, but a queued task-notification is the only
		// linkage trace when the session ended before injection.
		var payload string
		if err := json.Unmarshal(rec.Content, &payload); err == nil {
			p.harvestTaskNotification(payload)
		}
		p.account.SkippedRecords[rec.Type]++
	default:
		p.account.SkippedRecords[rec.Type]++
	}
}

// turnRecord maps one user/assistant record to a Turn, or accounts
// for why it could not.
func (p *parser) turnRecord(rec nativeRecord) {
	ts, tsErr := time.Parse(time.RFC3339, rec.Timestamp)
	if rec.UUID == "" || tsErr != nil {
		p.account.SkippedTurnRecords++
		return
	}
	var msg nativeMessage
	if err := json.Unmarshal(rec.Message, &msg); err != nil {
		p.account.SkippedTurnRecords++
		return
	}

	parts, toolResultOnly := p.parts(rec, msg)
	if len(parts) == 0 {
		p.account.SkippedTurnRecords++
		return
	}

	role := stem.RoleUser
	switch {
	case rec.Type == "assistant":
		role = stem.RoleAssistant
	case toolResultOnly:
		role = stem.RoleTool
	}

	p.turns = append(p.turns, stem.Turn{
		ID:        rec.UUID,
		Role:      role,
		CreatedAt: ts,
		Content:   parts,
		AgentID:   rec.AgentID,
	})
}

// parts maps the record's content blocks; toolResultOnly reports
// whether every mapped part is a tool_result (drives the tool role).
func (p *parser) parts(rec nativeRecord, msg nativeMessage) (parts []stem.ContentPart, toolResultOnly bool) {
	// Legacy vintage: content is a plain JSON string.
	var legacy string
	if err := json.Unmarshal(msg.Content, &legacy); err == nil {
		p.harvestTaskNotification(legacy)
		return []stem.ContentPart{stem.TextPart(legacy)}, false
	}

	var blocks []nativeBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil, false
	}

	// The completed-dispatch record names the sidechain this result
	// spawned; harvest it for linkage before mapping blocks.
	dispatchAgentID := agentIDFrom(rec.ToolUseResult)

	toolResultOnly = len(blocks) > 0
	for _, b := range blocks {
		part, ok := p.block(b, dispatchAgentID)
		if !ok {
			continue
		}
		if part.Type != stem.PartTypeToolResult {
			toolResultOnly = false
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		toolResultOnly = false
	}
	return parts, toolResultOnly
}

// block maps one content block. ok is false when the block was
// dropped (unknown type, orphan tool_result, unusable image).
func (p *parser) block(b nativeBlock, dispatchAgentID string) (part stem.ContentPart, ok bool) {
	switch b.Type {
	case "text":
		p.harvestTaskNotification(b.Text)
		return stem.TextPart(b.Text), true

	case "thinking":
		return stem.ContentPart{
			Type:      stem.PartTypeThinking,
			Text:      b.Thinking,
			Signature: b.Signature,
		}, true

	case "tool_use":
		p.openCalls[b.ID] = struct{}{}
		return stem.ContentPart{
			Type:   stem.PartTypeToolCall,
			CallID: b.ID,
			Name:   b.Name,
			Input:  b.Input,
		}, true

	case "tool_result":
		if _, open := p.openCalls[b.ToolUseID]; !open {
			p.account.OrphanToolResults++
			return stem.ContentPart{}, false
		}
		delete(p.openCalls, b.ToolUseID)
		output := b.Content
		if len(output) == 0 {
			output = json.RawMessage(`null`)
		}
		part = stem.ContentPart{
			Type:    stem.PartTypeToolResult,
			CallID:  b.ToolUseID,
			Output:  output,
			IsError: b.IsError,
		}
		if dispatchAgentID != "" {
			part.ChildEnvelopeID = dispatchAgentID
			p.refs[dispatchAgentID] = DispatchRef{
				EnvelopeID: p.sessionID,
				CallID:     b.ToolUseID,
			}
		}
		return part, true

	case "image":
		if b.Source == nil || b.Source.MediaType == "" {
			p.account.DroppedParts++
			return stem.ContentPart{}, false
		}
		switch b.Source.Type {
		case "base64":
			return stem.ContentPart{
				Type: stem.PartTypeImage,
				Mime: b.Source.MediaType,
				Data: b.Source.Data,
			}, true
		case "url":
			return stem.ContentPart{
				Type: stem.PartTypeImage,
				Mime: b.Source.MediaType,
				URL:  b.Source.URL,
			}, true
		default:
			p.account.DroppedParts++
			return stem.ContentPart{}, false
		}

	default:
		p.account.DroppedParts++
		return stem.ContentPart{}, false
	}
}

// harvestTaskNotification extracts background-dispatch linkage from
// a <task-notification> payload: <task-id> names the sidechain,
// <tool-use-id> the spawning tool call. Synchronous refs (from
// toolUseResult) are authoritative and never overwritten.
func (p *parser) harvestTaskNotification(text string) {
	if !strings.Contains(text, "<task-notification>") {
		return
	}
	agentID := xmlElement(text, "task-id")
	callID := xmlElement(text, "tool-use-id")
	if agentID == "" || callID == "" {
		return
	}
	if _, exists := p.refs[agentID]; exists {
		return
	}
	p.refs[agentID] = DispatchRef{EnvelopeID: p.sessionID, CallID: callID}
}

// xmlElement returns the trimmed text of the first <name>...</name>
// element in s, or "".
func xmlElement(s, name string) string {
	openTag, closeTag := "<"+name+">", "</"+name+">"
	start := strings.Index(s, openTag)
	if start < 0 {
		return ""
	}
	rest := s[start+len(openTag):]
	end := strings.Index(rest, closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// agentIDFrom extracts toolUseResult.agentId when present.
func agentIDFrom(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var probe struct {
		AgentID string `json:"agentId"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.AgentID
}

// result assembles the envelope from accumulated parser state.
func (p *parser) result(path, abs string, dispatch DispatchIndex) *Result {
	res := &Result{Account: p.account, DispatchRefs: p.refs}
	if len(p.turns) == 0 {
		return res
	}

	id := p.sessionID
	if p.sidechain {
		id = p.agentID
		if id == "" {
			id = strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), agentFilePrefix), ".jsonl")
		}
	} else if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}

	version := p.version
	if version == "" {
		version = unknownVersion
	}

	metadata := map[string]any{MetaNativePath: abs}
	if p.cwd != "" {
		metadata[MetaCWD] = p.cwd
	}
	if p.gitBranch != "" {
		metadata[MetaGitBranch] = p.gitBranch
	}

	env := &stem.Session{
		CrtxVersion: stem.CrtxVersion,
		ID:          id,
		CreatedAt:   p.turns[0].CreatedAt,
		UpdatedAt:   p.turns[len(p.turns)-1].CreatedAt,
		Source:      stem.Source{Kind: SourceKind, Version: version},
		Turns:       p.turns,
		Metadata:    metadata,
	}

	if p.sidechain {
		if ref, ok := dispatch[id]; ok && ref.EnvelopeID != "" && ref.CallID != "" {
			env.DispatchedFrom = &stem.DispatchedFrom{
				EnvelopeID: ref.EnvelopeID,
				CallID:     ref.CallID,
			}
		} else {
			res.Account.DispatchUnresolved = true
		}
	}

	res.Envelope = env
	return res
}
