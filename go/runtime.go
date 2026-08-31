package stem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrMaxToolRounds is returned when tool-call recursion exceeds the
// configured round budget. Message text preserved from the original
// ErrMaxToolDepth wording for backward compat.
var ErrMaxToolRounds = errors.New("stem: max tool depth exceeded")

// ErrMaxToolDepth is a deprecated alias for ErrMaxToolRounds, kept
// so existing errors.Is(err, ErrMaxToolDepth) call sites compile.
//
// Deprecated: use ErrMaxToolRounds.
var ErrMaxToolDepth = ErrMaxToolRounds

// ErrNoProvider is returned when Send/StreamSend is called without a
// configured Provider.
var ErrNoProvider = errors.New("stem: no provider configured")

// Runtime orchestrates conversation turns through a Provider.
// It wraps a Session (data) and adds tool dispatch, persistence, and
// event publication.
type Runtime struct {
	mu            sync.Mutex
	session       *Session
	provider      Provider
	store         Store
	publisher     Publisher
	toolRegistry  *ToolRegistry
	maxToolRounds int
	// agentID, when set, stamps Turn.AgentID on every assistant /
	// tool Turn this Runtime appends. User-role turns are left
	// unattributed per envelope.md §3.1. See RuntimeWithAgentID.
	agentID string
}

// RuntimeOption configures a Runtime.
type RuntimeOption func(*Runtime)

// NewRuntime creates a Runtime wrapping the given Session.
func NewRuntime(session *Session, opts ...RuntimeOption) *Runtime {
	r := &Runtime{
		session:       session,
		maxToolRounds: 10,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// RuntimeWithProvider sets the LLM provider on a Runtime.
func RuntimeWithProvider(p Provider) RuntimeOption {
	return func(r *Runtime) { r.provider = p }
}

// RuntimeWithStore sets the persistence backend on a Runtime.
func RuntimeWithStore(s Store) RuntimeOption {
	return func(r *Runtime) { r.store = s }
}

// RuntimeWithPublisher sets the event publisher on a Runtime.
func RuntimeWithPublisher(p Publisher) RuntimeOption {
	return func(r *Runtime) { r.publisher = p }
}

// RuntimeWithToolRegistry sets the tool registry for dispatch.
func RuntimeWithToolRegistry(reg *ToolRegistry) RuntimeOption {
	return func(r *Runtime) { r.toolRegistry = reg }
}

// RuntimeWithMaxToolRounds caps recursive tool-call loops at N tool
// rounds after the initial provider call. The total provider-call
// budget per Send is 1 + maxToolRounds (default 10). A round runs
// when the assistant turn issues at least one tool_call; the loop
// stops on a tool-call-free response.
func RuntimeWithMaxToolRounds(n int) RuntimeOption {
	return func(r *Runtime) {
		if n > 0 {
			r.maxToolRounds = n
		}
	}
}

// RuntimeWithMaxToolDepth is a deprecated alias for
// RuntimeWithMaxToolRounds, kept for v0.1.0 backward compat. Prefer
// the new name; semantics are identical to RuntimeWithMaxToolRounds.
//
// Deprecated: use RuntimeWithMaxToolRounds.
func RuntimeWithMaxToolDepth(n int) RuntimeOption {
	return RuntimeWithMaxToolRounds(n)
}

// RuntimeWithAgentID sets the producing-agent identifier this Runtime
// stamps on every assistant and tool Turn it appends. Use when an
// Envelope hosts multiple assistants (envelope.md §3.1). Stability
// across Turns within one Envelope is required; stability across
// Envelopes is recommended.
//
// user / system Turns are left unattributed per spec guidance.
func RuntimeWithAgentID(id string) RuntimeOption {
	return func(r *Runtime) { r.agentID = id }
}

// Session returns the underlying data session.
func (rt *Runtime) Session() *Session { return rt.session }

// Send appends a user message, calls Provider.Complete, handles any
// tool calls, and returns the final assistant Turn.
func (rt *Runtime) Send(ctx context.Context, content string) (Turn, error) {
	if rt.provider == nil {
		return Turn{}, ErrNoProvider
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// 1. Append user turn and persist immediately so a provider error
	//    doesn't lose the user input.
	userTurn := Turn{
		ID:        fmt.Sprintf("u-%d", len(rt.session.Turns)),
		Role:      RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{TextPart(content)},
	}
	rt.appendTurn(userTurn)
	if err := rt.persistTurn(ctx, userTurn); err != nil {
		return Turn{}, fmt.Errorf("stem: persist user turn: %w", err)
	}

	rt.emitEvent(ctx, TopicTurnUserReceived, turnUserPayload(rt.session.ID, userTurn))

	// 2. Call provider in a tool-call loop.
	assistantTurn, err := rt.completeWithToolLoop(ctx)
	if err != nil {
		return Turn{}, err
	}

	// 3. Append final assistant turn.
	assistantTurn.ID = fmt.Sprintf("a-%d", len(rt.session.Turns))
	if assistantTurn.CreatedAt.IsZero() {
		assistantTurn.CreatedAt = time.Now().UTC()
	}
	assistantTurn = rt.stampAgent(assistantTurn)
	rt.appendTurn(assistantTurn)

	rt.emitEvent(ctx, TopicTurnAssistantEmitted, turnAssistantPayload(rt.session.ID, assistantTurn))

	// 4. Persist assistant turn.
	if err := rt.persistTurn(ctx, assistantTurn); err != nil {
		return Turn{}, fmt.Errorf("stem: persist assistant turn: %w", err)
	}

	return assistantTurn, nil
}

// completeWithToolLoop calls Provider.Complete and dispatches tool
// calls until the provider returns a tool-call-free response or the
// round budget is exhausted. Budget is 1 initial provider call plus
// maxToolRounds tool rounds.
func (rt *Runtime) completeWithToolLoop(ctx context.Context) (Turn, error) {
	budget := 1 + rt.maxToolRounds
	for range budget {
		if err := ctx.Err(); err != nil {
			return Turn{}, err
		}
		resp, err := rt.provider.Complete(ctx, rt.session.Turns)
		if err != nil {
			return Turn{}, fmt.Errorf("stem: provider.Complete: %w", err)
		}

		calls := toolCallsIn(resp)
		if len(calls) == 0 || rt.toolRegistry == nil {
			return resp, nil
		}

		// Append the assistant Turn that issued the calls so the
		// transcript shows the request before the result. This matters
		// for crtx tool_result → tool_call linkage.
		assistantCallTurn := resp
		assistantCallTurn.ID = fmt.Sprintf("a-%d", len(rt.session.Turns))
		if assistantCallTurn.CreatedAt.IsZero() {
			assistantCallTurn.CreatedAt = time.Now().UTC()
		}
		assistantCallTurn = rt.stampAgent(assistantCallTurn)
		rt.appendTurn(assistantCallTurn)
		if err := rt.persistTurn(ctx, assistantCallTurn); err != nil {
			return Turn{}, fmt.Errorf("stem: persist assistant tool-call turn: %w", err)
		}

		// Dispatch tool calls; each result becomes a tool-role Turn.
		for _, c := range calls {
			rt.emitEvent(ctx, TopicTurnToolCalled, turnToolCalledPayload(
				rt.session.ID, assistantCallTurn.ID, assistantCallTurn.AgentID, c,
			))

			output, handleErr := rt.toolRegistry.Handle(
				ctx, c.Name, c.Input,
			)
			if output == nil {
				output = json.RawMessage(`null`)
			}

			resultPart, _ := ToolResultPart(c.CallID, output, handleErr != nil)
			if handleErr != nil {
				resultPart, _ = ToolResultPart(c.CallID, handleErr.Error(), true)
			}
			toolTurn := Turn{
				ID:        fmt.Sprintf("tool-%d", len(rt.session.Turns)),
				Role:      RoleTool,
				CreatedAt: time.Now().UTC(),
				Content:   []ContentPart{resultPart},
			}
			toolTurn = rt.stampAgent(toolTurn)
			rt.appendTurn(toolTurn)
			if err := rt.persistTurn(ctx, toolTurn); err != nil {
				return Turn{}, fmt.Errorf("stem: persist tool turn: %w", err)
			}

			rt.emitEvent(ctx, TopicTurnToolCompleted, turnToolCompletedPayload(rt.session.ID, toolTurn))
		}
	}

	return Turn{}, ErrMaxToolRounds
}

// toolCallsIn returns all tool_call ContentParts in a Turn's content.
func toolCallsIn(t Turn) []ContentPart {
	var out []ContentPart
	for _, p := range t.Content {
		if p.Type == PartTypeToolCall {
			out = append(out, p)
		}
	}
	return out
}

// StreamSend appends a user message and returns a TurnStream that
// yields tokens. On stream completion the finalized assistant Turn
// is appended and persisted.
//
// The mutex is NOT held across the stream lifetime. It is acquired
// only for state mutations (appending turns, persisting).
func (rt *Runtime) StreamSend(
	ctx context.Context, content string,
) (TurnStream, error) {
	if rt.provider == nil {
		return nil, ErrNoProvider
	}

	rt.mu.Lock()

	// 1. Append user turn and persist before streaming.
	userTurn := Turn{
		ID:        fmt.Sprintf("u-%d", len(rt.session.Turns)),
		Role:      RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{TextPart(content)},
	}
	rt.appendTurn(userTurn)
	if err := rt.persistTurn(ctx, userTurn); err != nil {
		rt.mu.Unlock()
		return nil, fmt.Errorf("stem: persist user turn: %w", err)
	}

	rt.emitEvent(ctx, TopicTurnUserReceived, turnUserPayload(rt.session.ID, userTurn))

	// 2. Call provider.Stream.
	stream, err := rt.provider.Stream(ctx, rt.session.Turns)
	rt.mu.Unlock() // Release before returning stream to caller.
	if err != nil {
		return nil, fmt.Errorf("stem: provider.Stream: %w", err)
	}

	// 3. Return a wrapping stream that finalizes on done.
	return &runtimeStream{
		rt:     rt,
		ctx:    ctx,
		inner:  stream,
		tokens: make([]Token, 0, 32),
	}, nil
}

// runtimeStream wraps a provider TurnStream, accumulates tokens, and
// finalizes the assistant turn on completion or close.
//
// Concurrency: Next() is typically driven by a reader goroutine while
// Close() may be called from a supervisor goroutine. The mu mutex
// protects finalized + err and serializes the at-most-once
// finalization sequence.
type runtimeStream struct {
	rt     *Runtime
	ctx    context.Context
	inner  TurnStream
	tokens []Token

	mu        sync.Mutex
	finalized bool
	// err captures the first persistence error encountered during
	// finalize. Callers can inspect it via Err() after Close.
	err error
}

// Err returns the first persistence error encountered during stream
// finalization, or nil. Callers can type-assert the TurnStream
// returned from StreamSend to *runtimeStream (or use the
// StreamErrorer interface) to surface deferred persistence errors.
func (s *runtimeStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// StreamErrorer is implemented by TurnStream values that capture
// deferred errors (e.g. persistence failures) during finalization.
type StreamErrorer interface {
	Err() error
}

func (s *runtimeStream) Next() (Token, error) {
	tok, err := s.inner.Next()
	if err != nil {
		return tok, err
	}

	s.tokens = append(s.tokens, tok)

	if tok.Done {
		s.finalize()
	}

	return tok, nil
}

func (s *runtimeStream) Close() error {
	s.finalize()
	return s.inner.Close()
}

func (s *runtimeStream) finalize() {
	s.mu.Lock()
	if s.finalized {
		s.mu.Unlock()
		return
	}
	s.finalized = true
	s.mu.Unlock()

	// Build content from accumulated tokens.
	var content string
	for _, t := range s.tokens {
		content += t.Text
	}

	s.rt.mu.Lock()
	assistantTurn := Turn{
		ID:        fmt.Sprintf("a-%d", len(s.rt.session.Turns)),
		Role:      RoleAssistant,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{TextPart(content)},
	}
	assistantTurn = s.rt.stampAgent(assistantTurn)
	s.rt.appendTurn(assistantTurn)
	s.rt.session.UpdatedAt = time.Now().UTC()
	s.rt.mu.Unlock()

	s.rt.emitEvent(s.ctx, TopicTurnAssistantEmitted, turnAssistantPayload(s.rt.session.ID, assistantTurn))
	if perr := s.rt.persistTurn(s.ctx, assistantTurn); perr != nil {
		s.mu.Lock()
		if s.err == nil {
			s.err = fmt.Errorf("stem: persist assistant turn: %w", perr)
		}
		s.mu.Unlock()
	}
}

// appendTurn adds a turn to the session (caller holds mu).
func (rt *Runtime) appendTurn(t Turn) {
	rt.session.Turns = append(rt.session.Turns, t)
	rt.session.UpdatedAt = time.Now().UTC()
}

// stampAgent fills in Turn.AgentID from the Runtime's configured
// agentID when applicable (assistant / tool / developer roles only,
// per envelope.md §3.1). user / system Turns are left unattributed.
// Returns the (possibly mutated) Turn so callers can chain.
func (rt *Runtime) stampAgent(t Turn) Turn {
	if rt.agentID == "" || t.AgentID != "" {
		return t
	}
	switch t.Role {
	case RoleAssistant, RoleTool, RoleDeveloper:
		t.AgentID = rt.agentID
	}
	return t
}

// emitEvent fires an event if a publisher is configured.
func (rt *Runtime) emitEvent(
	ctx context.Context, topic string, payload any,
) {
	if rt.publisher != nil {
		_ = rt.publisher.Publish(ctx, topic, payload)
	}
}

// persistTurn appends a turn to the store if configured.
func (rt *Runtime) persistTurn(ctx context.Context, t Turn) error {
	if rt.store == nil {
		return nil
	}
	return rt.store.AppendTurn(ctx, rt.session.ID, t)
}
