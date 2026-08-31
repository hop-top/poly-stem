package stem

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Dispatch tests ---
//
// Dispatch creates a dispatch-nest child Envelope (envelope.md §7.2).
// The child sets DispatchedFrom = { envelope_id, call_id } and does
// NOT inherit parent transcript; it does inherit Store/Publisher/Router.

func TestDispatch_CreatesChildWithDispatchedFrom(t *testing.T) {
	parent := NewSession("parent-1")
	seedOpenToolCall(t, parent, "call-1")

	ctx := context.Background()
	child := parent.Dispatch(ctx, "child-1", "call-1")

	assert.Equal(t, "child-1", child.ID)
	assert.Empty(t, child.ParentID, "dispatch nest must NOT set parent_id")
	assert.Nil(t, child.ForkPoint, "dispatch nest must NOT set fork_point")
	require.NotNil(t, child.DispatchedFrom)
	assert.Equal(t, "parent-1", child.DispatchedFrom.EnvelopeID)
	assert.Equal(t, "call-1", child.DispatchedFrom.CallID)
	assert.Equal(t, CrtxVersion, child.CrtxVersion)
	assert.NotZero(t, child.CreatedAt)
	assert.Nil(t, child.ClosedAt)
}

func TestDispatch_ChildStartsEmpty(t *testing.T) {
	parent := NewSession("parent-1")
	parent.Turns = append(parent.Turns, Turn{
		ID: "t-1", Role: RoleUser, CreatedAt: time.Now().UTC(),
		Content: []ContentPart{TextPart("hello")},
	})
	seedOpenToolCall(t, parent, "call-1")

	child := parent.Dispatch(context.Background(), "child-1", "call-1")

	assert.Empty(t, child.Turns, "dispatch nest must not inherit parent turns")
	assert.Len(t, parent.Turns, 2, "parent turns unchanged")
}

func TestDispatch_ParentTracksChildren(t *testing.T) {
	parent := NewSession("parent-1")
	seedOpenToolCall(t, parent, "call-a")
	seedOpenToolCall(t, parent, "call-b")

	parent.Dispatch(context.Background(), "c-1", "call-a")
	parent.Dispatch(context.Background(), "c-2", "call-b")

	assert.Equal(t, []string{"c-1", "c-2"}, parent.Children)
}

func TestDispatch_InheritsStore(t *testing.T) {
	store := NewMemoryStore()
	parent := NewSession("parent-1", WithStore(store))
	seedOpenToolCall(t, parent, "call-1")

	child := parent.Dispatch(context.Background(), "child-1", "call-1")
	seedOpenToolCall(t, child, "call-2")
	grandchild := child.Dispatch(context.Background(), "grandchild-1", "call-2")
	require.NotNil(t, grandchild.DispatchedFrom)
	assert.Equal(t, "child-1", grandchild.DispatchedFrom.EnvelopeID)
}

func TestDispatch_OverridesProvider(t *testing.T) {
	parent := NewSession("parent-1",
		WithProvider(&stubProvider{name: "parent-llm"}),
	)

	seedOpenToolCall(t, parent, "call-1")

	childProv := &stubProvider{name: "child-llm"}
	child := parent.Dispatch(context.Background(), "child-1", "call-1",
		WithProvider(childProv),
	)

	assert.Equal(t, "child-1", child.ID)
	require.NotNil(t, child.DispatchedFrom)
	assert.Equal(t, "parent-1", child.DispatchedFrom.EnvelopeID)
}

func TestDispatch_PublishesNestedTopic(t *testing.T) {
	pub := &recordingPublisher{}
	parent := NewSession("parent-1", WithPublisher(pub))
	seedOpenToolCall(t, parent, "call-1")

	parent.Dispatch(context.Background(), "child-1", "call-1")

	require.Len(t, pub.events, 1)
	assert.Equal(t, TopicSessionEnvelopeNested, pub.events[0].topic)
	payload, ok := pub.events[0].payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "parent-1", payload["parent_id"])
	assert.Equal(t, "child-1", payload["child_id"])
	assert.Equal(t, "call-1", payload["call_id"])
	// events.md §3.9 requires created_at as RFC 3339 string.
	ts, ok := payload["created_at"].(string)
	require.True(t, ok, "created_at must be string")
	_, err := time.Parse(time.RFC3339Nano, ts)
	require.NoError(t, err)
}

func TestDispatch_ChildValidates(t *testing.T) {
	parent := NewSession("parent-1")
	seedOpenToolCall(t, parent, "call-1")
	child := parent.Dispatch(context.Background(), "child-1", "call-1")

	// Spec §7.2: dispatched_from is mutually exclusive with
	// parent_id/fork_point. The child must validate on its own.
	assert.NoError(t, Validate(child))
}

// Dispatch rejects callIDs that don't correspond to an open tool_call
// in the parent's transcript: an unknown callID and an
// already-resolved (tool_result-closed) callID both fail per
// envelope.md §7.2.

func TestDispatch_RejectsUnknownCallID(t *testing.T) {
	parent := NewSession("parent-1")

	assert.Panics(t, func() {
		parent.Dispatch(context.Background(), "child-1", "call-unknown")
	}, "Dispatch must panic when callID is not in parent transcript")
}

// Validator (validate.go) accepts the legal sequence
// [call(X), result(X), call(X)] by re-adding X to openCalls after
// the tool_result clears it. Dispatch's open-call check must mirror
// that: walking the transcript past the tool_result and seeing the
// re-armed tool_call MUST leave the call considered open, NOT closed.
func TestDispatch_AcceptsReopenedCallID(t *testing.T) {
	parent := NewSession("parent-1")
	// First call.
	callPart1, err := ToolCallPart("call-x", "demo_tool", map[string]any{})
	require.NoError(t, err)
	parent.Turns = append(parent.Turns, Turn{
		ID:        "t-call-1",
		Role:      RoleAssistant,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{callPart1},
	})
	// Result closes it.
	resultPart, err := ToolResultPart("call-x", "ok", false)
	require.NoError(t, err)
	parent.Turns = append(parent.Turns, Turn{
		ID:        "t-result",
		Role:      RoleTool,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{resultPart},
	})
	// Second call with same id re-arms it.
	callPart2, err := ToolCallPart("call-x", "demo_tool", map[string]any{})
	require.NoError(t, err)
	parent.Turns = append(parent.Turns, Turn{
		ID:        "t-call-2",
		Role:      RoleAssistant,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{callPart2},
	})

	assert.NotPanics(t, func() {
		parent.Dispatch(context.Background(), "child-1", "call-x")
	}, "Dispatch must accept a re-armed call_id (validator-legal reopen)")
}

func TestDispatch_RejectsEmptyCallID(t *testing.T) {
	parent := NewSession("parent-1")

	assert.PanicsWithValue(t,
		"stem: Dispatch: call_id must not be empty (envelope.md §7.2)",
		func() {
			parent.Dispatch(context.Background(), "child-1", "")
		},
		"Dispatch must panic with a dedicated message on empty call_id",
	)
}

func TestDispatch_RejectsAlreadyClosedCallID(t *testing.T) {
	parent := NewSession("parent-1")
	// Append a tool_call and its matching tool_result so the call is
	// closed at the moment of dispatch.
	callPart, err := ToolCallPart("call-closed", "demo_tool", map[string]any{})
	require.NoError(t, err)
	parent.Turns = append(parent.Turns, Turn{
		ID:        "t-call",
		Role:      RoleAssistant,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{callPart},
	})
	resultPart, err := ToolResultPart("call-closed", "ok", false)
	require.NoError(t, err)
	parent.Turns = append(parent.Turns, Turn{
		ID:        "t-result",
		Role:      RoleTool,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{resultPart},
	})

	assert.Panics(t, func() {
		parent.Dispatch(context.Background(), "child-1", "call-closed")
	}, "Dispatch must panic when callID is already closed by a tool_result")
}

// --- ReturnDispatch tests ---
//
// ReturnDispatch folds a dispatch-nest child's result back into the
// parent's transcript: appends a tool_result closing the open call and
// publishes crtx.session.envelope.returned per events.md §3.10.

// returnDispatchSetup wires a parent with a single open tool_call and
// dispatches a child against it. Returns parent, child, and the
// recordingPublisher so individual tests can assert on the call/result
// shape and emitted topics.
func returnDispatchSetup(t *testing.T) (
	parent, child *Session, pub *recordingPublisher,
) {
	t.Helper()
	pub = &recordingPublisher{}
	parent = NewSession("parent-1", WithPublisher(pub))

	callPart, err := ToolCallPart("call-1", "do_work", map[string]any{"q": 1})
	require.NoError(t, err)
	parent.Turns = append(parent.Turns, Turn{
		ID: "t-call-1", Role: RoleAssistant, CreatedAt: time.Now().UTC(),
		Content: []ContentPart{callPart},
	})

	child = parent.Dispatch(context.Background(), "child-1", "call-1")
	// Drop the nested event from the recorder so subsequent assertions
	// can scope to ReturnDispatch's own emissions.
	pub.mu.Lock()
	pub.events = nil
	pub.mu.Unlock()
	return parent, child, pub
}

func TestReturnDispatch_AppendsToolResult(t *testing.T) {
	parent, child, _ := returnDispatchSetup(t)

	err := parent.ReturnDispatch(
		context.Background(), child, "call-1",
		map[string]any{"ok": true}, false,
	)
	require.NoError(t, err)

	// Parent should now carry the original tool_call turn plus the
	// tool_result turn we just appended.
	require.Len(t, parent.Turns, 2)
	result := parent.Turns[1]
	assert.Equal(t, RoleTool, result.Role)
	require.Len(t, result.Content, 1)
	part := result.Content[0]
	assert.Equal(t, PartTypeToolResult, part.Type)
	assert.Equal(t, "call-1", part.CallID)
	assert.Equal(t, "child-1", part.ChildEnvelopeID)
	assert.False(t, part.IsError)
	assert.JSONEq(t, `{"ok":true}`, string(part.Output))
}

func TestReturnDispatch_PublishesReturnedTopic(t *testing.T) {
	parent, child, pub := returnDispatchSetup(t)

	err := parent.ReturnDispatch(
		context.Background(), child, "call-1",
		map[string]any{"ok": true}, false,
	)
	require.NoError(t, err)

	pub.mu.Lock()
	defer pub.mu.Unlock()
	require.Len(t, pub.events, 1)
	assert.Equal(t, TopicSessionEnvelopeReturned, pub.events[0].topic)

	payload, ok := pub.events[0].payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "parent-1", payload["parent_id"])
	assert.Equal(t, "child-1", payload["child_id"])
	assert.Equal(t, "call-1", payload["call_id"])
	// events.md §3.10 requires created_at as RFC 3339 string.
	ts, ok := payload["created_at"].(string)
	require.True(t, ok, "created_at must be string")
	_, perr := time.Parse(time.RFC3339Nano, ts)
	require.NoError(t, perr)
	// is_error defaults to false; omitted from payload to match the
	// shape used by the sibling nested topic (only set fields land in
	// the map). Spec §3.10 marks it optional with default false.
	_, hasIsError := payload["is_error"]
	assert.False(t, hasIsError)
}

func TestReturnDispatch_IsErrorPropagates(t *testing.T) {
	parent, child, pub := returnDispatchSetup(t)

	err := parent.ReturnDispatch(
		context.Background(), child, "call-1",
		map[string]any{"err": "boom"}, true,
	)
	require.NoError(t, err)

	require.Len(t, parent.Turns, 2)
	assert.True(t, parent.Turns[1].Content[0].IsError)

	pub.mu.Lock()
	defer pub.mu.Unlock()
	require.Len(t, pub.events, 1)
	payload := pub.events[0].payload.(map[string]any)
	assert.Equal(t, true, payload["is_error"])
}

func TestReturnDispatch_PersistsViaStore(t *testing.T) {
	store := NewMemoryStore()
	pub := &recordingPublisher{}
	parent := NewSession(
		"parent-1",
		WithStore(store),
		WithPublisher(pub),
	)
	ctx := context.Background()
	require.NoError(t, store.Create(ctx, SessionMeta{ID: "parent-1"}))

	callPart, err := ToolCallPart("call-1", "do_work", map[string]any{})
	require.NoError(t, err)
	require.NoError(t, parent.AppendTurnPersisted(ctx, Turn{
		ID: "t-call-1", Role: RoleAssistant, CreatedAt: time.Now().UTC(),
		Content: []ContentPart{callPart},
	}))

	child := parent.Dispatch(ctx, "child-1", "call-1")

	require.NoError(t, parent.ReturnDispatch(
		ctx, child, "call-1", map[string]any{"ok": true}, false,
	))

	loaded, err := store.Load(ctx, "parent-1")
	require.NoError(t, err)
	require.Len(t, loaded.Turns, 2)
	assert.Equal(t, RoleTool, loaded.Turns[1].Role)
	assert.Equal(t, "child-1", loaded.Turns[1].Content[0].ChildEnvelopeID)
}

func TestReturnDispatch_RejectsNonDispatchChild(t *testing.T) {
	parent, _, _ := returnDispatchSetup(t)
	// A session that was never dispatched from this parent.
	stranger := NewSession("stranger")

	err := parent.ReturnDispatch(
		context.Background(), stranger, "call-1", nil, false,
	)
	assert.ErrorIs(t, err, ErrNotDispatchChild)
	assert.Len(t, parent.Turns, 1, "no tool_result appended on rejection")
}

func TestReturnDispatch_RejectsMismatchedCallID(t *testing.T) {
	parent, child, _ := returnDispatchSetup(t)
	// Child carries DispatchedFrom.CallID = "call-1"; caller passes a
	// different call_id — must reject even though the child itself is
	// a legit dispatch nest.
	err := parent.ReturnDispatch(
		context.Background(), child, "call-other", nil, false,
	)
	assert.ErrorIs(t, err, ErrNotDispatchChild)
}

func TestReturnDispatch_RejectsAlreadyResolvedCall(t *testing.T) {
	parent, child, _ := returnDispatchSetup(t)

	ctx := context.Background()
	require.NoError(t, parent.ReturnDispatch(
		ctx, child, "call-1", map[string]any{"ok": true}, false,
	))

	// Second return for the same call must fail: the tool_result has
	// already closed it.
	err := parent.ReturnDispatch(
		ctx, child, "call-1", map[string]any{"ok": true}, false,
	)
	assert.ErrorIs(t, err, ErrCallNotOpen)
	assert.Len(t, parent.Turns, 2, "only one tool_result appended")
}

func TestReturnDispatch_RejectsUnknownCall(t *testing.T) {
	// Parent with no tool_call in transcript at all; child claims to
	// be a dispatch nest of "call-1" but the parent never opened it.
	// Dispatch now panics on unknown callID (envelope.md §7.2), so we
	// construct the child by hand to exercise ReturnDispatch's own
	// open-call guard.
	parent := NewSession("parent-1")
	child := &Session{
		CrtxVersion: CrtxVersion,
		ID:          "child-1",
		DispatchedFrom: &DispatchedFrom{
			EnvelopeID: parent.ID,
			CallID:     "call-1",
		},
	}

	err := parent.ReturnDispatch(
		context.Background(), child, "call-1", nil, false,
	)
	assert.ErrorIs(t, err, ErrCallNotOpen)
}

// --- Router / SendTo tests ---

func TestSendTo_DeliversTurn(t *testing.T) {
	router := NewDirectRouter()

	sender := NewSession("sender", WithRouter(router))
	receiver := NewSession("receiver", WithRouter(router))

	router.Register(sender)
	router.Register(receiver)

	ctx := context.Background()
	err := sender.SendTo(ctx, "receiver", "hello from sender")
	require.NoError(t, err)

	assert.Len(t, receiver.Turns, 1)
	assert.Equal(t, RoleUser, receiver.Turns[0].Role)
	require.Len(t, receiver.Turns[0].Content, 1)
	assert.Equal(t, "hello from sender", receiver.Turns[0].Content[0].Text)
	assert.Equal(t, "sender", receiver.Turns[0].Metadata[MetaKeyFromSession])
}

// SendTo must route through the receiver's persisted-append so the
// receiver's store contains the routed turn after the call.
func TestSendTo_RoutesThroughStore(t *testing.T) {
	store := NewMemoryStore()
	router := NewDirectRouter()

	sender := NewSession("sender", WithRouter(router))
	receiver := NewSession("receiver",
		WithRouter(router),
		WithStore(store),
	)

	ctx := context.Background()
	require.NoError(t, store.Create(ctx, SessionMeta{ID: "receiver"}))

	router.Register(sender)
	router.Register(receiver)

	require.NoError(t, sender.SendTo(ctx, "receiver", "ping"))

	loaded, err := store.Load(ctx, "receiver")
	require.NoError(t, err)
	require.Len(t, loaded.Turns, 1)
	assert.Equal(t, "ping", loaded.Turns[0].Content[0].Text)
	assert.Equal(t, "sender", loaded.Turns[0].Metadata[MetaKeyFromSession])
}

func TestSendTo_UnknownTargetErrors(t *testing.T) {
	router := NewDirectRouter()
	sender := NewSession("sender", WithRouter(router))
	router.Register(sender)

	err := sender.SendTo(context.Background(), "nobody", "msg")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSendTo_NoRouterErrors(t *testing.T) {
	s := NewSession("lonely")

	err := s.SendTo(context.Background(), "target", "msg")
	assert.ErrorIs(t, err, ErrRouterNotSet)
}

func TestDirectRouter_RegisterUnregister(t *testing.T) {
	router := NewDirectRouter()
	s := NewSession("s-1")

	router.Register(s)
	router.Unregister("s-1")

	err := router.Route(context.Background(), "other", "s-1", Turn{
		ID: "t-1", Role: RoleUser, CreatedAt: time.Now().UTC(),
		Content: []ContentPart{TextPart("test")},
	})
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

// --- Supervisor tests ---

func TestSupervisor_WatchRegisters(t *testing.T) {
	sv := NewSupervisor(nil)
	child := NewSession("child-1")

	ctx, cancel, err := sv.Watch(context.Background(), child)
	require.NoError(t, err)
	assert.NotNil(t, ctx)
	defer cancel()

	_, _, err = sv.Watch(context.Background(), child)
	assert.ErrorIs(t, err, ErrAlreadyWatched)
}

func TestSupervisor_CancelStopsChild(t *testing.T) {
	sv := NewSupervisor(nil)
	child := NewSession("child-1")

	childCtx, _, err := sv.Watch(context.Background(), child)
	require.NoError(t, err)

	require.NoError(t, sv.Cancel("child-1"))

	select {
	case <-childCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("child context should be canceled")
	}
}

func TestSupervisor_CancelUnknownErrors(t *testing.T) {
	sv := NewSupervisor(nil)
	err := sv.Cancel("unknown")
	assert.ErrorIs(t, err, ErrChildNotFound)
}

func TestSupervisor_WaitAllBlocks(t *testing.T) {
	sv := NewSupervisor(nil)

	c1 := NewSession("c-1")
	c2 := NewSession("c-2")

	_, _, err := sv.Watch(context.Background(), c1)
	require.NoError(t, err)
	_, _, err = sv.Watch(context.Background(), c2)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- sv.WaitAll(context.Background())
	}()

	select {
	case <-done:
		t.Fatal("WaitAll returned before children completed")
	case <-time.After(50 * time.Millisecond):
	}

	require.NoError(t, sv.Done("c-1"))
	require.NoError(t, sv.Done("c-2"))

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("WaitAll did not return after children completed")
	}
}

func TestSupervisor_WaitAllRespectsCtx(t *testing.T) {
	sv := NewSupervisor(nil)
	child := NewSession("c-1")
	_, _, err := sv.Watch(context.Background(), child)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = sv.WaitAll(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSupervisor_OnChildDoneFires(t *testing.T) {
	pub := &recordingPublisher{}
	sv := NewSupervisor(pub)

	var mu sync.Mutex
	var completedIDs []string
	sv.OnChildDone(func(id string) {
		mu.Lock()
		completedIDs = append(completedIDs, id)
		mu.Unlock()
	})

	child := NewSession("c-1")
	_, _, err := sv.Watch(context.Background(), child)
	require.NoError(t, err)

	require.NoError(t, sv.Done("c-1"))

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, []string{"c-1"}, completedIDs)
	mu.Unlock()

	pub.mu.Lock()
	defer pub.mu.Unlock()
	require.Len(t, pub.events, 1)
	assert.Equal(t, TopicStemChildDone, pub.events[0].topic)
}

// --- Test helpers ---

// seedOpenToolCall appends an assistant Turn whose only ContentPart
// is a tool_call with the given callID, leaving the call open (no
// matching tool_result). Used by Dispatch tests to satisfy the §7.2
// open-tool_call precondition.
func seedOpenToolCall(t *testing.T, parent *Session, callID string) {
	t.Helper()
	part, err := ToolCallPart(callID, "seeded_tool", map[string]any{})
	require.NoError(t, err)
	parent.Turns = append(parent.Turns, Turn{
		ID:        "seed-" + callID,
		Role:      RoleAssistant,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{part},
	})
}

type stubProvider struct {
	name string
}

func (p *stubProvider) Complete(
	_ context.Context, _ []Turn,
) (Turn, error) {
	return Turn{
		ID: "resp-1", Role: RoleAssistant,
		Content: []ContentPart{TextPart("response from " + p.name)},
	}, nil
}

func (p *stubProvider) Stream(
	_ context.Context, _ []Turn,
) (TurnStream, error) {
	return nil, nil
}

func (p *stubProvider) CallWithTools(
	_ context.Context, _ []Turn, _ []ToolDef,
) (Turn, error) {
	return Turn{}, nil
}
