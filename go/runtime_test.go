package stem

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// --- mock provider ---

type mockProvider struct {
	mu        sync.Mutex
	calls     int
	responses []Turn
}

func newMockProvider(responses ...Turn) *mockProvider {
	return &mockProvider{responses: responses}
}

func (m *mockProvider) Complete(
	_ context.Context, _ []Turn,
) (Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.calls >= len(m.responses) {
		return Turn{
			Role:    RoleAssistant,
			Content: []ContentPart{TextPart("fallback")},
		}, nil
	}
	resp := m.responses[m.calls]
	m.calls++
	return resp, nil
}

func (m *mockProvider) Stream(
	_ context.Context, turns []Turn,
) (TurnStream, error) {
	resp, err := m.Complete(context.Background(), turns)
	if err != nil {
		return nil, err
	}
	text := ""
	for _, p := range resp.Content {
		if p.Type == PartTypeText {
			text += p.Text
		}
	}
	return &mockStream{tokens: splitTokens(text)}, nil
}

func (m *mockProvider) CallWithTools(
	ctx context.Context, turns []Turn, _ []ToolDef,
) (Turn, error) {
	return m.Complete(ctx, turns)
}

// --- mock stream ---

type mockStream struct {
	tokens []Token
	idx    int
}

func splitTokens(content string) []Token {
	if content == "" {
		return []Token{{Done: true}}
	}
	var tokens []Token
	for _, ch := range content {
		tokens = append(tokens, Token{Text: string(ch)})
	}
	tokens = append(tokens, Token{Done: true})
	return tokens
}

func (s *mockStream) Next() (Token, error) {
	if s.idx >= len(s.tokens) {
		return Token{Done: true}, nil
	}
	tok := s.tokens[s.idx]
	s.idx++
	return tok, nil
}

func (s *mockStream) Close() error { return nil }

// --- mock publisher ---

type mockPublisher struct {
	mu     sync.Mutex
	events []pubEvent
}

type pubEvent struct {
	Topic   string
	Payload any
}

func (p *mockPublisher) Publish(
	_ context.Context, topic string, payload any,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, pubEvent{Topic: topic, Payload: payload})
	return nil
}

func (p *mockPublisher) topics() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, e := range p.events {
		out = append(out, e.Topic)
	}
	return out
}

func newSession(id string) *Session {
	now := time.Now().UTC()
	return &Session{
		CrtxVersion: CrtxVersion,
		ID:          id,
		CreatedAt:   now,
		UpdatedAt:   now,
		Source:      DefaultSource(),
	}
}

// --- tests ---

func TestSend_AppendsUserAndAssistantTurns(t *testing.T) {
	sess := newSession("s-1")
	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{TextPart("hi there")},
	})

	rt := NewRuntime(sess, RuntimeWithProvider(prov))
	ctx := context.Background()

	got, err := rt.Send(ctx, "hello")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.Role != RoleAssistant {
		t.Fatalf("expected assistant role, got %s", got.Role)
	}
	if got.Content[0].Text != "hi there" {
		t.Fatalf("expected 'hi there', got %q", got.Content[0].Text)
	}

	turns := rt.Session().Turns
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if turns[0].Role != RoleUser {
		t.Fatalf("first turn should be user, got %s", turns[0].Role)
	}
	if turns[1].Role != RoleAssistant {
		t.Fatalf("second turn should be assistant, got %s", turns[1].Role)
	}
}

func TestSend_NoToolsSingleRoundTrip(t *testing.T) {
	sess := newSession("s-1")
	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{TextPart("done")},
	})

	rt := NewRuntime(sess, RuntimeWithProvider(prov))
	_, err := rt.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	prov.mu.Lock()
	calls := prov.calls
	prov.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 provider call, got %d", calls)
	}
}

func TestSend_ToolCallInvokesHandler(t *testing.T) {
	toolCall, err := ToolCallPart("call_echo", "echo", map[string]any{"msg": "ping"})
	if err != nil {
		t.Fatalf("ToolCallPart: %v", err)
	}
	prov := newMockProvider(
		Turn{
			Role:    RoleAssistant,
			Content: []ContentPart{toolCall},
		},
		Turn{
			Role:    RoleAssistant,
			Content: []ContentPart{TextPart("pong received")},
		},
	)

	reg := NewToolRegistry()
	handlerCalled := false
	reg.Register("echo", ToolHandlerFunc(
		func(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
			handlerCalled = true
			return json.RawMessage(`{"reply":"pong"}`), nil
		},
	))

	sess := newSession("s-1")
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithToolRegistry(reg),
	)

	got, err := rt.Send(context.Background(), "do echo")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !handlerCalled {
		t.Fatal("expected tool handler to be called")
	}
	if got.Content[0].Text != "pong received" {
		t.Fatalf("expected 'pong received', got %q", got.Content[0].Text)
	}

	// Verify tool turn exists and its tool_result.call_id matches.
	turns := rt.Session().Turns
	if len(turns) < 4 {
		t.Fatalf("expected at least 4 turns (user, assistant-call, tool, assistant-final), got %d", len(turns))
	}

	foundTool := false
	for _, turn := range turns {
		if turn.Role == RoleTool {
			foundTool = true
			if turn.Content[0].Type != PartTypeToolResult {
				t.Fatalf("tool turn must carry tool_result, got %q", turn.Content[0].Type)
			}
			if turn.Content[0].CallID != "call_echo" {
				t.Fatalf("tool_result.call_id mismatch: %q", turn.Content[0].CallID)
			}
		}
	}
	if !foundTool {
		t.Fatal("expected a tool turn in session")
	}

	// The envelope must validate.
	if err := Validate(rt.Session()); err != nil {
		t.Fatalf("session does not validate: %v", err)
	}
}

func TestSend_MaxToolDepthRespected(t *testing.T) {
	prov := &infiniteToolProvider{}

	reg := NewToolRegistry()
	reg.Register("loop", ToolHandlerFunc(
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	))

	sess := newSession("s-1")
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithToolRegistry(reg),
		RuntimeWithMaxToolRounds(3),
	)

	_, err := rt.Send(context.Background(), "go")
	if !errors.Is(err, ErrMaxToolDepth) {
		t.Fatalf("expected ErrMaxToolDepth, got: %v", err)
	}
}

// One legit tool round (provider responds with a tool_call, handler
// runs, follow-up provider call is text-only) must succeed when
// maxToolRounds=1. Regression guard for the off-by-one in the old
// "depth" semantics.
func TestSend_OneToolRoundWithMaxToolRoundsOne(t *testing.T) {
	tc, err := ToolCallPart("call_x", "echo", map[string]any{})
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
			return json.RawMessage(`{}`), nil
		},
	))

	sess := newSession("s-1")
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithToolRegistry(reg),
		RuntimeWithMaxToolRounds(1),
	)

	got, err := rt.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.Content[0].Text != "done" {
		t.Fatalf("expected 'done', got %q", got.Content[0].Text)
	}
}

// Cancellation mid-tool-loop must surface context.Canceled without
// invoking Provider.Complete on the next iteration.
func TestSend_ContextCancelStopsToolLoop(t *testing.T) {
	tc, _ := ToolCallPart("call_x", "loop", map[string]any{})
	prov := &cancelingProvider{
		tc: tc,
	}

	reg := NewToolRegistry()
	reg.Register("loop", ToolHandlerFunc(
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	))

	sess := newSession("s-1")
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithToolRegistry(reg),
		RuntimeWithMaxToolRounds(5),
	)

	ctx, cancel := context.WithCancel(context.Background())
	prov.cancelOnComplete = cancel

	_, err := rt.Send(ctx, "go")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

type cancelingProvider struct {
	tc               ContentPart
	cancelOnComplete context.CancelFunc
}

func (p *cancelingProvider) Complete(
	_ context.Context, _ []Turn,
) (Turn, error) {
	// Issue a tool call, then cancel ctx so the next loop iteration
	// must short-circuit before the next Provider.Complete.
	if p.cancelOnComplete != nil {
		p.cancelOnComplete()
		p.cancelOnComplete = nil
	}
	return Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{p.tc},
	}, nil
}

func (p *cancelingProvider) Stream(
	_ context.Context, _ []Turn,
) (TurnStream, error) {
	return nil, errors.New("not implemented")
}

func (p *cancelingProvider) CallWithTools(
	ctx context.Context, turns []Turn, _ []ToolDef,
) (Turn, error) {
	return p.Complete(ctx, turns)
}

type infiniteToolProvider struct{}

func (p *infiniteToolProvider) Complete(
	_ context.Context, _ []Turn,
) (Turn, error) {
	tc, _ := ToolCallPart("tc-inf", "loop", map[string]any{})
	return Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{tc},
	}, nil
}

func (p *infiniteToolProvider) Stream(
	_ context.Context, _ []Turn,
) (TurnStream, error) {
	return nil, errors.New("not implemented")
}

func (p *infiniteToolProvider) CallWithTools(
	ctx context.Context, turns []Turn, _ []ToolDef,
) (Turn, error) {
	return p.Complete(ctx, turns)
}

func TestSend_PersistsToStore(t *testing.T) {
	sess := newSession("s-1")
	store := NewMemoryStore()
	ctx := context.Background()

	if err := store.Create(ctx, SessionMeta{ID: "s-1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{TextPart("stored")},
	})

	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithStore(store),
	)

	_, err := rt.Send(ctx, "save this")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	loaded, err := store.Load(ctx, "s-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Turns) < 2 {
		t.Fatalf("expected at least 2 persisted turns, got %d", len(loaded.Turns))
	}
}

func TestSend_PublishesEvents(t *testing.T) {
	sess := newSession("s-1")
	pub := &mockPublisher{}
	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{TextPart("ok")},
	})

	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
	)

	_, err := rt.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	topics := pub.topics()
	if len(topics) < 2 {
		t.Fatalf("expected at least 2 events, got %d: %v", len(topics), topics)
	}

	hasTurnStart := false
	hasTurnDone := false
	for _, topic := range topics {
		switch topic {
		case TopicTurnUserReceived:
			hasTurnStart = true
		case TopicTurnAssistantEmitted:
			hasTurnDone = true
		}
	}
	if !hasTurnStart {
		t.Fatal("expected crtx.turn.user.received event")
	}
	if !hasTurnDone {
		t.Fatal("expected crtx.turn.assistant.emitted event")
	}
}

func TestSend_NilPublisherNoPanic(t *testing.T) {
	sess := newSession("s-1")
	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{TextPart("ok")},
	})

	rt := NewRuntime(sess, RuntimeWithProvider(prov))

	_, err := rt.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestSend_NoProviderError(t *testing.T) {
	sess := newSession("s-1")
	rt := NewRuntime(sess) // no provider

	_, err := rt.Send(context.Background(), "hello")
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("expected ErrNoProvider, got: %v", err)
	}
}

func TestSend_ToolCallPublishesToolEvents(t *testing.T) {
	pub := &mockPublisher{}
	tc, _ := ToolCallPart("tc-ping", "ping", map[string]any{})
	prov := newMockProvider(
		Turn{
			Role:    RoleAssistant,
			Content: []ContentPart{tc},
		},
		Turn{Role: RoleAssistant, Content: []ContentPart{TextPart("done")}},
	)

	reg := NewToolRegistry()
	reg.Register("ping", ToolHandlerFunc(
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{}`), nil
		},
	))

	sess := newSession("s-1")
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
		RuntimeWithToolRegistry(reg),
	)

	_, err := rt.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	topics := pub.topics()
	hasToolCalled := false
	hasToolDone := false
	for _, topic := range topics {
		switch topic {
		case TopicTurnToolCalled:
			hasToolCalled = true
		case TopicTurnToolCompleted:
			hasToolDone = true
		}
	}
	if !hasToolCalled {
		t.Fatal("expected session.tool.called event")
	}
	if !hasToolDone {
		t.Fatal("expected session.tool.done event")
	}
}

// --- Persistence-error propagation tests ---

// failingStore returns an error on AppendTurn while behaving as a
// MemoryStore for all other operations. Used to verify that Send /
// StreamSend surface persistence errors instead of silently dropping
// them.
type failingStore struct {
	*MemoryStore
	appendErr   error
	failOnRoles map[Role]bool
}

func newFailingStore(err error, roles ...Role) *failingStore {
	m := map[Role]bool{}
	for _, r := range roles {
		m[r] = true
	}
	return &failingStore{
		MemoryStore: NewMemoryStore(),
		appendErr:   err,
		failOnRoles: m,
	}
}

func (s *failingStore) AppendTurn(ctx context.Context, id string, t Turn) error {
	if s.failOnRoles[t.Role] {
		return s.appendErr
	}
	return s.MemoryStore.AppendTurn(ctx, id, t)
}

func TestSend_PropagatesPersistUserTurnError(t *testing.T) {
	store := newFailingStore(errors.New("disk full"), RoleUser)
	ctx := context.Background()
	if err := store.Create(ctx, SessionMeta{ID: "s-1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess := newSession("s-1")
	prov := newMockProvider(Turn{
		Role: RoleAssistant, Content: []ContentPart{TextPart("ok")},
	})
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithStore(store),
	)

	_, err := rt.Send(ctx, "hello")
	if err == nil {
		t.Fatal("expected persist user turn error")
	}
	if got := err.Error(); !contains(got, "persist user turn") {
		t.Fatalf("expected persist user turn error, got: %v", err)
	}
}

func TestSend_PersistsUserTurnBeforeProviderError(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.Create(ctx, SessionMeta{ID: "s-1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess := newSession("s-1")
	prov := &erroringProvider{err: errors.New("backend down")}
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithStore(store),
	)

	_, err := rt.Send(ctx, "hello")
	if err == nil {
		t.Fatal("expected provider error")
	}

	loaded, err := store.Load(ctx, "s-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Turns) != 1 {
		t.Fatalf("expected user turn persisted before provider error, got %d turns", len(loaded.Turns))
	}
	if loaded.Turns[0].Role != RoleUser {
		t.Fatalf("expected user turn, got %s", loaded.Turns[0].Role)
	}
}

func TestStreamSend_SurfacesPersistError(t *testing.T) {
	// User turn persists fine; finalize fails on assistant.
	store := newFailingStore(errors.New("disk full"), RoleAssistant)
	ctx := context.Background()
	if err := store.Create(ctx, SessionMeta{ID: "s-1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess := newSession("s-1")
	prov := newMockProvider(Turn{
		Role: RoleAssistant, Content: []ContentPart{TextPart("hi")},
	})
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithStore(store),
	)

	stream, err := rt.StreamSend(ctx, "go")
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

	se, ok := stream.(StreamErrorer)
	if !ok {
		t.Fatal("expected runtimeStream to implement StreamErrorer")
	}
	if se.Err() == nil {
		t.Fatal("expected deferred persist error from stream.Err()")
	}
}

type erroringProvider struct {
	err error
}

func (p *erroringProvider) Complete(_ context.Context, _ []Turn) (Turn, error) {
	return Turn{}, p.err
}

func (p *erroringProvider) Stream(_ context.Context, _ []Turn) (TurnStream, error) {
	return nil, p.err
}

func (p *erroringProvider) CallWithTools(
	_ context.Context, _ []Turn, _ []ToolDef,
) (Turn, error) {
	return Turn{}, p.err
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- StreamSend tests ---

func TestStreamSend_ReturnsTokens(t *testing.T) {
	sess := newSession("s-1")
	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{TextPart("abc")},
	})

	rt := NewRuntime(sess, RuntimeWithProvider(prov))

	stream, err := rt.StreamSend(context.Background(), "go")
	if err != nil {
		t.Fatalf("StreamSend: %v", err)
	}

	var text string
	for {
		tok, err := stream.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		text += tok.Text
		if tok.Done {
			break
		}
	}

	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if text != "abc" {
		t.Fatalf("expected 'abc', got %q", text)
	}
}

func TestStreamSend_FinalizesAssistantTurn(t *testing.T) {
	sess := newSession("s-1")
	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{TextPart("hello")},
	})

	rt := NewRuntime(sess, RuntimeWithProvider(prov))

	stream, err := rt.StreamSend(context.Background(), "go")
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

	turns := rt.Session().Turns
	if len(turns) < 2 {
		t.Fatalf("expected at least 2 turns, got %d", len(turns))
	}

	last := turns[len(turns)-1]
	if last.Role != RoleAssistant {
		t.Fatalf("expected last turn to be assistant, got %s", last.Role)
	}
	if last.Content[0].Text != "hello" {
		t.Fatalf("expected 'hello', got %q", last.Content[0].Text)
	}
}

func TestStreamSend_NoProviderError(t *testing.T) {
	sess := newSession("s-1")
	rt := NewRuntime(sess)

	_, err := rt.StreamSend(context.Background(), "hello")
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("expected ErrNoProvider, got: %v", err)
	}
}

// TestAppendTurnPersisted_RollsBackOnStoreFailure verifies that a
// store error reverts the in-memory append, so the caller never sees
// disk lagging the in-memory transcript.
func TestAppendTurnPersisted_RollsBackOnStoreFailure(t *testing.T) {
	ctx := context.Background()
	store := newFailingStore(errors.New("disk full"), RoleUser)
	if err := store.Create(ctx, SessionMeta{ID: "s-1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess := NewSession("s-1", WithStore(store))
	sess.UpdatedAt = sess.CreatedAt
	prevUpdatedAt := sess.UpdatedAt

	turn := Turn{
		ID:        "t-0",
		Role:      RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{TextPart("hi")},
	}
	err := sess.AppendTurnPersisted(ctx, turn)
	if err == nil {
		t.Fatal("expected store failure to propagate")
	}
	if !contains(err.Error(), "disk full") {
		t.Fatalf("expected wrapped 'disk full', got: %v", err)
	}
	if got := len(sess.Turns); got != 0 {
		t.Fatalf("expected in-memory rollback, got %d turns", got)
	}
	if !sess.UpdatedAt.Equal(prevUpdatedAt) {
		t.Fatalf("expected UpdatedAt rollback")
	}
}

// TestRuntimeStream_ConcurrentErrAccess exercises concurrent
// readers/writers against the err and finalized fields. Run with
// -race to detect missing synchronization; the production usage pulls
// tokens in one goroutine while a supervisor may call Close from
// another.
func TestRuntimeStream_ConcurrentErrAccess(t *testing.T) {
	sess := newSession("s-1")
	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{TextPart("abc")},
	})
	store := newFailingStore(errors.New("disk full"), RoleAssistant)
	ctx := context.Background()
	if err := store.Create(ctx, SessionMeta{ID: "s-1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithStore(store),
	)

	stream, err := rt.StreamSend(ctx, "go")
	if err != nil {
		t.Fatalf("StreamSend: %v", err)
	}
	se := stream.(StreamErrorer)

	deadline := time.Now().Add(50 * time.Millisecond)
	var wg sync.WaitGroup
	wg.Add(2)

	// Reader: drains tokens (triggers finalize via Done token), then
	// keeps polling Err().
	go func() {
		defer wg.Done()
		for {
			tok, err := stream.Next()
			if err != nil {
				return
			}
			if tok.Done {
				break
			}
		}
		for time.Now().Before(deadline) {
			_ = se.Err()
		}
	}()

	// Supervisor: races Close + Err reads.
	go func() {
		defer wg.Done()
		for time.Now().Before(deadline) {
			_ = se.Err()
		}
		_ = stream.Close()
	}()

	wg.Wait()
	if se.Err() == nil {
		t.Fatal("expected deferred persist error after concurrent run")
	}
}

func TestStreamSend_PublishesEvents(t *testing.T) {
	pub := &mockPublisher{}
	sess := newSession("s-1")
	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		Content: []ContentPart{TextPart("ok")},
	})

	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithPublisher(pub),
	)

	stream, err := rt.StreamSend(context.Background(), "go")
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

	topics := pub.topics()
	hasTurnStart := false
	hasTurnDone := false
	for _, topic := range topics {
		switch topic {
		case TopicTurnUserReceived:
			hasTurnStart = true
		case TopicTurnAssistantEmitted:
			hasTurnDone = true
		}
	}
	if !hasTurnStart {
		t.Fatal("expected crtx.turn.user.received event")
	}
	if !hasTurnDone {
		t.Fatal("expected crtx.turn.assistant.emitted event")
	}
}

// TestRuntimeWithAgentID — assistant + tool turns produced through
// the Runtime carry the configured agent_id; user turns do not.
// envelope.md §3.1.
func TestRuntimeWithAgentID(t *testing.T) {
	toolCall, err := ToolCallPart("call_echo", "echo", map[string]any{"msg": "ping"})
	if err != nil {
		t.Fatalf("ToolCallPart: %v", err)
	}
	prov := newMockProvider(
		Turn{Role: RoleAssistant, Content: []ContentPart{toolCall}},
		Turn{Role: RoleAssistant, Content: []ContentPart{TextPart("done")}},
	)

	reg := NewToolRegistry()
	reg.Register("echo", ToolHandlerFunc(
		func(_ context.Context, _ string, in json.RawMessage) (json.RawMessage, error) {
			return in, nil
		},
	))

	sess := newSession("s-1")
	rt := NewRuntime(sess,
		RuntimeWithProvider(prov),
		RuntimeWithToolRegistry(reg),
		RuntimeWithAgentID("worker-a"),
	)
	if _, err := rt.Send(context.Background(), "go"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	turns := rt.Session().Turns
	if len(turns) < 3 {
		t.Fatalf("expected at least 3 turns (user, assistant-call, tool); got %d", len(turns))
	}

	// User Turn carries no agent_id; assistant + tool turns do.
	if turns[0].Role != RoleUser || turns[0].AgentID != "" {
		t.Fatalf("turn[0] should be user with empty AgentID; got %s / %q", turns[0].Role, turns[0].AgentID)
	}
	for i, tt := range turns[1:] {
		if tt.Role == RoleUser {
			continue
		}
		if tt.AgentID != "worker-a" {
			t.Fatalf("turn[%d] (%s): AgentID = %q, want worker-a", i+1, tt.Role, tt.AgentID)
		}
	}
}

// TestRuntimeAgentIDPreservedWhenProviderSetsIt — when the provider
// returns a Turn that already carries AgentID, the Runtime must not
// overwrite it. Reserved for multi-agent provider impls.
func TestRuntimeAgentIDPreservedWhenProviderSetsIt(t *testing.T) {
	prov := newMockProvider(Turn{
		Role:    RoleAssistant,
		AgentID: "provider-supplied",
		Content: []ContentPart{TextPart("hi")},
	})
	rt := NewRuntime(newSession("s-1"),
		RuntimeWithProvider(prov),
		RuntimeWithAgentID("runtime-default"),
	)
	if _, err := rt.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	turns := rt.Session().Turns
	if got := turns[len(turns)-1].AgentID; got != "provider-supplied" {
		t.Fatalf("expected provider's agent_id to win; got %q", got)
	}
}
