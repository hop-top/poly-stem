package stem

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// recordingPublisher / recordedEvent live in testhelpers_test.go and
// are shared with multi_test.go.

// TestPayloadConformance_TurnUserReceived verifies the emission for
// crtx.turn.user.received carries envelope_id, turn_id, content per
// events.md §3.1; optional in_reply_to_call_id omitted when unset.
func TestPayloadConformance_TurnUserReceived(t *testing.T) {
	sess := newSession("env-1")
	pub := &recordingPublisher{}
	prov := newMockProvider(Turn{
		Role: RoleAssistant, Content: []ContentPart{TextPart("ok")},
	})
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
	)

	if _, err := rt.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	events := pub.find(TopicTurnUserReceived)
	if len(events) != 1 {
		t.Fatalf("expected 1 user.received event, got %d", len(events))
	}
	payload, ok := events[0].payload.(map[string]any)
	if !ok {
		t.Fatalf("payload must be map[string]any, got %T", events[0].payload)
	}

	if payload["envelope_id"] != "env-1" {
		t.Errorf("envelope_id = %v, want env-1", payload["envelope_id"])
	}
	if payload["turn_id"] != "u-0" {
		t.Errorf("turn_id = %v, want u-0", payload["turn_id"])
	}
	content, ok := payload["content"].([]ContentPart)
	if !ok {
		t.Fatalf("content must be []ContentPart, got %T", payload["content"])
	}
	if len(content) != 1 || content[0].Text != "hello" {
		t.Errorf("content[0].Text = %q, want hello", content[0].Text)
	}

	if _, has := payload["in_reply_to_call_id"]; has {
		t.Errorf("optional in_reply_to_call_id must be omitted when unset")
	}
	if _, has := payload["agent_id"]; has {
		t.Errorf("user payload has no agent_id field per spec §3.1")
	}
}

// TestPayloadConformance_TurnAssistantEmitted_NoAgent verifies the
// emission for crtx.turn.assistant.emitted with no agent_id set.
func TestPayloadConformance_TurnAssistantEmitted_NoAgent(t *testing.T) {
	sess := newSession("env-1")
	pub := &recordingPublisher{}
	prov := newMockProvider(Turn{
		Role: RoleAssistant, Content: []ContentPart{TextPart("hi back")},
	})
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
	)

	if _, err := rt.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	events := pub.find(TopicTurnAssistantEmitted)
	if len(events) != 1 {
		t.Fatalf("expected 1 assistant.emitted event, got %d", len(events))
	}
	payload := events[0].payload.(map[string]any)

	if payload["envelope_id"] != "env-1" {
		t.Errorf("envelope_id = %v, want env-1", payload["envelope_id"])
	}
	if got, _ := payload["turn_id"].(string); got == "" {
		t.Errorf("turn_id must be set, got %v", payload["turn_id"])
	}
	if _, ok := payload["content"].([]ContentPart); !ok {
		t.Errorf("content must be []ContentPart, got %T", payload["content"])
	}

	if _, has := payload["agent_id"]; has {
		t.Errorf("optional agent_id must be omitted when unset")
	}
	if _, has := payload["in_reply_to_call_id"]; has {
		t.Errorf("optional in_reply_to_call_id must be omitted when unset")
	}
}

// TestPayloadConformance_TurnAssistantEmitted_WithAgent verifies that
// when Runtime.agentID is set, the assistant payload carries agent_id.
func TestPayloadConformance_TurnAssistantEmitted_WithAgent(t *testing.T) {
	sess := newSession("env-1")
	pub := &recordingPublisher{}
	prov := newMockProvider(Turn{
		Role: RoleAssistant, Content: []ContentPart{TextPart("hi back")},
	})
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
		RuntimeWithAgentID("worker-a"),
	)

	if _, err := rt.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	events := pub.find(TopicTurnAssistantEmitted)
	if len(events) != 1 {
		t.Fatalf("expected 1 assistant.emitted event, got %d", len(events))
	}
	payload := events[0].payload.(map[string]any)

	if payload["agent_id"] != "worker-a" {
		t.Errorf("agent_id = %v, want worker-a", payload["agent_id"])
	}
}

// TestPayloadConformance_TurnToolCalled verifies the emission for
// crtx.turn.tool.called per §3.3 — turn_id is the assistant Turn
// that produced the call; call_id/name/input required; optional
// parent_call_id and agent_id omitted when unset.
func TestPayloadConformance_TurnToolCalled(t *testing.T) {
	tc, err := ToolCallPart("call_echo", "echo", map[string]any{"msg": "ping"})
	if err != nil {
		t.Fatalf("ToolCallPart: %v", err)
	}
	prov := newMockProvider(
		Turn{Role: RoleAssistant, Content: []ContentPart{tc}},
		Turn{Role: RoleAssistant, Content: []ContentPart{TextPart("done")}},
	)

	reg := NewToolRegistry()
	reg.Register("echo", ToolHandlerFunc(
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"reply":"pong"}`), nil
		},
	))

	sess := newSession("env-1")
	pub := &recordingPublisher{}
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
		RuntimeWithToolRegistry(reg),
	)

	if _, err := rt.Send(context.Background(), "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	events := pub.find(TopicTurnToolCalled)
	if len(events) != 1 {
		t.Fatalf("expected 1 tool.called event, got %d", len(events))
	}
	payload := events[0].payload.(map[string]any)

	if payload["envelope_id"] != "env-1" {
		t.Errorf("envelope_id = %v, want env-1", payload["envelope_id"])
	}
	// turn_id is the assistant Turn that produced the call. With one
	// user turn appended first (u-0), the assistant tool-call turn is
	// a-1.
	if payload["turn_id"] != "a-1" {
		t.Errorf("turn_id = %v, want a-1 (assistant turn id)", payload["turn_id"])
	}
	if payload["call_id"] != "call_echo" {
		t.Errorf("call_id = %v, want call_echo", payload["call_id"])
	}
	if payload["name"] != "echo" {
		t.Errorf("name = %v, want echo", payload["name"])
	}
	if _, has := payload["input"]; !has {
		t.Errorf("input field required by §3.3")
	}

	if _, has := payload["parent_call_id"]; has {
		t.Errorf("optional parent_call_id must be omitted when unset")
	}
	if _, has := payload["agent_id"]; has {
		t.Errorf("optional agent_id must be omitted when unset")
	}
}

// TestPayloadConformance_TurnToolCalled_WithAgent verifies that when
// Runtime.agentID is set, the tool.called payload carries agent_id
// (stamped on the producing assistant turn).
func TestPayloadConformance_TurnToolCalled_WithAgent(t *testing.T) {
	tc, _ := ToolCallPart("call_x", "echo", map[string]any{})
	prov := newMockProvider(
		Turn{Role: RoleAssistant, Content: []ContentPart{tc}},
		Turn{Role: RoleAssistant, Content: []ContentPart{TextPart("done")}},
	)

	reg := NewToolRegistry()
	reg.Register("echo", ToolHandlerFunc(
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	))

	sess := newSession("env-1")
	pub := &recordingPublisher{}
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
		RuntimeWithToolRegistry(reg),
		RuntimeWithAgentID("worker-a"),
	)

	if _, err := rt.Send(context.Background(), "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	events := pub.find(TopicTurnToolCalled)
	if len(events) != 1 {
		t.Fatalf("expected 1 tool.called event, got %d", len(events))
	}
	payload := events[0].payload.(map[string]any)

	if payload["agent_id"] != "worker-a" {
		t.Errorf("agent_id = %v, want worker-a", payload["agent_id"])
	}
}

// TestPayloadConformance_TurnToolCompleted verifies the emission for
// crtx.turn.tool.completed per §3.4 — call_id/output required;
// optional is_error/child_envelope_id/agent_id omitted when unset.
func TestPayloadConformance_TurnToolCompleted(t *testing.T) {
	tc, _ := ToolCallPart("call_x", "echo", map[string]any{})
	prov := newMockProvider(
		Turn{Role: RoleAssistant, Content: []ContentPart{tc}},
		Turn{Role: RoleAssistant, Content: []ContentPart{TextPart("done")}},
	)

	reg := NewToolRegistry()
	reg.Register("echo", ToolHandlerFunc(
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"reply":"pong"}`), nil
		},
	))

	sess := newSession("env-1")
	pub := &recordingPublisher{}
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
		RuntimeWithToolRegistry(reg),
	)

	if _, err := rt.Send(context.Background(), "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	events := pub.find(TopicTurnToolCompleted)
	if len(events) != 1 {
		t.Fatalf("expected 1 tool.completed event, got %d", len(events))
	}
	payload := events[0].payload.(map[string]any)

	if payload["envelope_id"] != "env-1" {
		t.Errorf("envelope_id = %v, want env-1", payload["envelope_id"])
	}
	if got, _ := payload["turn_id"].(string); got == "" {
		t.Errorf("turn_id must be set, got %v", payload["turn_id"])
	}
	if payload["call_id"] != "call_x" {
		t.Errorf("call_id = %v, want call_x", payload["call_id"])
	}
	output, ok := payload["output"].(json.RawMessage)
	if !ok {
		t.Fatalf("output must be json.RawMessage, got %T", payload["output"])
	}
	if string(output) != `{"reply":"pong"}` {
		t.Errorf("output = %s, want {\"reply\":\"pong\"}", output)
	}

	if _, has := payload["is_error"]; has {
		t.Errorf("optional is_error must be omitted when false")
	}
	if _, has := payload["child_envelope_id"]; has {
		t.Errorf("optional child_envelope_id must be omitted when unset")
	}
	if _, has := payload["agent_id"]; has {
		t.Errorf("optional agent_id must be omitted when unset")
	}
}

// TestPayloadConformance_TurnToolCompleted_IsError verifies that when
// the tool handler errors, is_error: true appears in the payload.
func TestPayloadConformance_TurnToolCompleted_IsError(t *testing.T) {
	tc, _ := ToolCallPart("call_x", "fail", map[string]any{})
	prov := newMockProvider(
		Turn{Role: RoleAssistant, Content: []ContentPart{tc}},
		Turn{Role: RoleAssistant, Content: []ContentPart{TextPart("done")}},
	)

	reg := NewToolRegistry()
	reg.Register("fail", ToolHandlerFunc(
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, &toolErr{msg: "boom"}
		},
	))

	sess := newSession("env-1")
	pub := &recordingPublisher{}
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
		RuntimeWithToolRegistry(reg),
	)

	if _, err := rt.Send(context.Background(), "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	events := pub.find(TopicTurnToolCompleted)
	if len(events) != 1 {
		t.Fatalf("expected 1 tool.completed event, got %d", len(events))
	}
	payload := events[0].payload.(map[string]any)

	if payload["is_error"] != true {
		t.Errorf("is_error = %v, want true", payload["is_error"])
	}
}

type toolErr struct{ msg string }

func (e *toolErr) Error() string { return e.msg }

// TestPayloadConformance_TurnUserReceived_Stream verifies the
// StreamSend path emits the same spec-shaped payload.
func TestPayloadConformance_TurnUserReceived_Stream(t *testing.T) {
	sess := newSession("env-1")
	pub := &recordingPublisher{}
	prov := newMockProvider(Turn{
		Role: RoleAssistant, Content: []ContentPart{TextPart("ok")},
	})
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
	)

	stream, err := rt.StreamSend(context.Background(), "hello")
	if err != nil {
		t.Fatalf("StreamSend: %v", err)
	}
	for {
		tok, err := stream.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if tok.Done {
			break
		}
	}
	_ = stream.Close()

	for _, e := range pub.find(TopicTurnUserReceived) {
		payload, ok := e.payload.(map[string]any)
		if !ok {
			t.Fatalf("user.received payload must be map, got %T", e.payload)
		}
		if payload["envelope_id"] != "env-1" {
			t.Errorf("envelope_id = %v, want env-1", payload["envelope_id"])
		}
	}

	for _, e := range pub.find(TopicTurnAssistantEmitted) {
		payload, ok := e.payload.(map[string]any)
		if !ok {
			t.Fatalf("assistant.emitted payload must be map, got %T", e.payload)
		}
		if payload["envelope_id"] != "env-1" {
			t.Errorf("envelope_id = %v, want env-1", payload["envelope_id"])
		}
	}
}

// TestPayloadHelpers_TurnThinking verifies the thinking payload
// builder directly. ThinkingPart isn't yet emitted by the Runtime;
// pin the helper shape for when it lands.
func TestPayloadHelpers_TurnThinking(t *testing.T) {
	t1 := Turn{ID: "a-1", Role: RoleAssistant}
	p := ContentPart{Type: PartTypeThinking, Text: "reasoning..."}
	out := turnThinkingPayload("env-1", t1, p)
	if out["envelope_id"] != "env-1" {
		t.Errorf("envelope_id = %v, want env-1", out["envelope_id"])
	}
	if out["turn_id"] != "a-1" {
		t.Errorf("turn_id = %v, want a-1", out["turn_id"])
	}
	if out["text"] != "reasoning..." {
		t.Errorf("text = %v, want reasoning...", out["text"])
	}
	if _, has := out["signature"]; has {
		t.Errorf("optional signature must be omitted when empty")
	}
	if _, has := out["agent_id"]; has {
		t.Errorf("optional agent_id must be omitted when empty")
	}
	if _, has := out["in_reply_to_call_id"]; has {
		t.Errorf("optional in_reply_to_call_id must be omitted when empty")
	}

	// With all optionals set.
	t2 := Turn{ID: "a-2", AgentID: "worker", InReplyToCallID: "c-1"}
	p2 := ContentPart{Type: PartTypeThinking, Text: "more", Signature: "sig"}
	out2 := turnThinkingPayload("env-1", t2, p2)
	if out2["signature"] != "sig" {
		t.Errorf("signature = %v, want sig", out2["signature"])
	}
	if out2["agent_id"] != "worker" {
		t.Errorf("agent_id = %v, want worker", out2["agent_id"])
	}
	if out2["in_reply_to_call_id"] != "c-1" {
		t.Errorf("in_reply_to_call_id = %v, want c-1", out2["in_reply_to_call_id"])
	}
}

// TestPayloadHelpers_TurnSystem verifies the system payload builder.
// crtx.turn.system.injected isn't yet emitted by the Runtime; pin
// the helper shape for when it lands.
func TestPayloadHelpers_TurnSystem(t *testing.T) {
	t1 := Turn{
		ID:      "s-1",
		Role:    RoleSystem,
		Content: []ContentPart{TextPart("guardrail")},
	}
	out := turnSystemPayload("env-1", t1)
	if out["envelope_id"] != "env-1" {
		t.Errorf("envelope_id = %v, want env-1", out["envelope_id"])
	}
	if out["turn_id"] != "s-1" {
		t.Errorf("turn_id = %v, want s-1", out["turn_id"])
	}
	content := out["content"].([]ContentPart)
	if content[0].Text != "guardrail" {
		t.Errorf("content[0].Text = %v, want guardrail", content[0].Text)
	}
	if _, has := out["in_reply_to_call_id"]; has {
		t.Errorf("optional in_reply_to_call_id must be omitted when empty")
	}
}

// TestPayloadHelpers_InReplyToCallID verifies that when the user
// Turn carries InReplyToCallID, it surfaces in the payload.
func TestPayloadHelpers_InReplyToCallID(t *testing.T) {
	t1 := Turn{
		ID:              "u-1",
		Role:            RoleUser,
		Content:         []ContentPart{TextPart("ping")},
		InReplyToCallID: "call-42",
	}
	out := turnUserPayload("env-1", t1)
	if out["in_reply_to_call_id"] != "call-42" {
		t.Errorf("in_reply_to_call_id = %v, want call-42", out["in_reply_to_call_id"])
	}
}

// TestPayloadHelpers_ToolCompletedChildEnvelopeID verifies that when
// the tool_result carries child_envelope_id, it surfaces in the
// payload (dispatch-nest tool result).
func TestPayloadHelpers_ToolCompletedChildEnvelopeID(t *testing.T) {
	t1 := Turn{
		ID:   "tool-1",
		Role: RoleTool,
		Content: []ContentPart{{
			Type:            PartTypeToolResult,
			CallID:          "c-1",
			Output:          json.RawMessage(`"done"`),
			ChildEnvelopeID: "child-env-1",
		}},
	}
	out := turnToolCompletedPayload("env-1", t1)
	if out["child_envelope_id"] != "child-env-1" {
		t.Errorf("child_envelope_id = %v, want child-env-1", out["child_envelope_id"])
	}
}

// TestPayloadHelpers_ToolCalledParentCallID verifies that when the
// tool_call part carries parent_call_id (nested call), it surfaces
// in the payload.
func TestPayloadHelpers_ToolCalledParentCallID(t *testing.T) {
	p := ContentPart{
		Type:         PartTypeToolCall,
		CallID:       "c-2",
		Name:         "inner",
		Input:        json.RawMessage(`{}`),
		ParentCallID: "c-1",
	}
	out := turnToolCalledPayload("env-1", "a-1", "", p)
	if out["parent_call_id"] != "c-1" {
		t.Errorf("parent_call_id = %v, want c-1", out["parent_call_id"])
	}
}

// TestPayloadHelpers_ToolCalledPanicsOnNonToolCall verifies the
// programmer-error contract: passing a non-tool_call ContentPart
// panics rather than silently emitting a malformed payload.
func TestPayloadHelpers_ToolCalledPanicsOnNonToolCall(t *testing.T) {
	p := ContentPart{Type: PartTypeText, Text: "hi"}
	assert.Panics(t, func() {
		_ = turnToolCalledPayload("env-1", "a-1", "", p)
	})
}

// TestPayloadHelpers_ToolCompletedPanicsOnMissingResult verifies the
// programmer-error contract: a tool-role Turn without a ToolResultPart
// panics rather than silently emitting a malformed payload.
func TestPayloadHelpers_ToolCompletedPanicsOnMissingResult(t *testing.T) {
	t1 := Turn{
		ID:      "tool-1",
		Role:    RoleTool,
		Content: []ContentPart{TextPart("not a result")},
	}
	assert.Panics(t, func() {
		_ = turnToolCompletedPayload("env-1", t1)
	})
}
