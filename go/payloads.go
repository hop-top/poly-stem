package stem

import (
	"encoding/json"
	"fmt"
)

// Spec-shaped event payload builders (events.md §3).
//
// Each canonical crtx.turn.* topic carries a specific payload field
// set: {envelope_id, turn_id, ...} with optional fields (agent_id,
// in_reply_to_call_id, parent_call_id, is_error, child_envelope_id)
// omitted when zero/empty. These helpers build the maps that
// Publisher subscribers see, so they MUST follow the field shapes
// pinned in §3.1–§3.10 verbatim.
//
// Optional fields are omitted (not zero/null) when unset — matches
// the canonicalPart pattern in ts/src/parse.ts.

// turnUserPayload builds the payload for crtx.turn.user.received
// (§3.1). Content is the Turn's content slice.
func turnUserPayload(envelopeID string, t Turn) map[string]any {
	out := map[string]any{
		"envelope_id": envelopeID,
		"turn_id":     t.ID,
		"content":     t.Content,
	}
	if t.InReplyToCallID != "" {
		out["in_reply_to_call_id"] = t.InReplyToCallID
	}
	return out
}

// turnAssistantPayload builds the payload for
// crtx.turn.assistant.emitted (§3.2). Optional agent_id and
// in_reply_to_call_id omitted when unset.
func turnAssistantPayload(envelopeID string, t Turn) map[string]any {
	out := map[string]any{
		"envelope_id": envelopeID,
		"turn_id":     t.ID,
		"content":     t.Content,
	}
	if t.AgentID != "" {
		out["agent_id"] = t.AgentID
	}
	if t.InReplyToCallID != "" {
		out["in_reply_to_call_id"] = t.InReplyToCallID
	}
	return out
}

// turnToolCalledPayload builds the §3.3 payload from a tool_call
// ContentPart produced by an assistant Turn. Panics if c.Type is not
// PartTypeToolCall (programmer-error contract — callers must invoke
// only on tool_call parts surfaced by toolCallsIn).
func turnToolCalledPayload(
	envelopeID, turnID, agentID string, c ContentPart,
) map[string]any {
	if c.Type != PartTypeToolCall {
		panic(fmt.Sprintf(
			"stem: turnToolCalledPayload: ContentPart type %q is not %q",
			c.Type, PartTypeToolCall,
		))
	}
	// Input is required by the spec; coerce empty to {} so subscribers
	// see a JSON object rather than null.
	input := c.Input
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	out := map[string]any{
		"envelope_id": envelopeID,
		"turn_id":     turnID,
		"call_id":     c.CallID,
		"name":        c.Name,
		"input":       input,
	}
	if c.ParentCallID != "" {
		out["parent_call_id"] = c.ParentCallID
	}
	if agentID != "" {
		out["agent_id"] = agentID
	}
	return out
}

// turnToolCompletedPayload builds the §3.4 payload from a tool-role
// Turn carrying a single ToolResultPart. Panics if the Turn has no
// ToolResultPart (programmer-error contract — callers must invoke
// only on completed tool turns).
func turnToolCompletedPayload(envelopeID string, t Turn) map[string]any {
	var resultPart *ContentPart
	for i := range t.Content {
		if t.Content[i].Type == PartTypeToolResult {
			resultPart = &t.Content[i]
			break
		}
	}
	if resultPart == nil {
		panic(fmt.Sprintf(
			"stem: turnToolCompletedPayload: turn %q has no tool_result ContentPart",
			t.ID,
		))
	}

	output := resultPart.Output
	if len(output) == 0 {
		output = json.RawMessage(`null`)
	}
	out := map[string]any{
		"envelope_id": envelopeID,
		"turn_id":     t.ID,
		"call_id":     resultPart.CallID,
		"output":      output,
	}
	if resultPart.IsError {
		out["is_error"] = true
	}
	if resultPart.ChildEnvelopeID != "" {
		out["child_envelope_id"] = resultPart.ChildEnvelopeID
	}
	if t.AgentID != "" {
		out["agent_id"] = t.AgentID
	}
	return out
}

// turnThinkingPayload builds the payload for
// crtx.turn.thinking.emitted (§3.5). Optional signature, agent_id,
// in_reply_to_call_id omitted when unset.
func turnThinkingPayload(
	envelopeID string, t Turn, p ContentPart,
) map[string]any {
	out := map[string]any{
		"envelope_id": envelopeID,
		"turn_id":     t.ID,
		"text":        p.Text,
	}
	if p.Signature != "" {
		out["signature"] = p.Signature
	}
	if t.AgentID != "" {
		out["agent_id"] = t.AgentID
	}
	if t.InReplyToCallID != "" {
		out["in_reply_to_call_id"] = t.InReplyToCallID
	}
	return out
}

// turnSystemPayload builds the payload for crtx.turn.system.injected
// (§3.6).
func turnSystemPayload(envelopeID string, t Turn) map[string]any {
	out := map[string]any{
		"envelope_id": envelopeID,
		"turn_id":     t.ID,
		"content":     t.Content,
	}
	if t.InReplyToCallID != "" {
		out["in_reply_to_call_id"] = t.InReplyToCallID
	}
	return out
}
