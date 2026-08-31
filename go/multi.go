package stem

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Multi-agent errors.
var (
	ErrRouterNotSet   = errors.New("stem: router not set on session")
	ErrChildNotFound  = errors.New("stem: child session not found")
	ErrAlreadyWatched = errors.New("stem: session already watched")
	// ErrNotDispatchChild is returned by ReturnDispatch when the child
	// passed in was not produced by Dispatch on this parent — either
	// DispatchedFrom is nil, points at a different parent, or carries
	// a mismatched call_id.
	ErrNotDispatchChild = errors.New("stem: session is not a dispatch nest of this parent")
	// ErrCallNotOpen is returned by ReturnDispatch when the supplied
	// callID does not correspond to an unresolved tool_call in the
	// parent's transcript (no matching tool_call, or already closed
	// by a prior tool_result with the same call_id).
	ErrCallNotOpen = errors.New("stem: tool_call is not open on this parent")
)

// MetaKeyFromSession is the Turn.Metadata key under which SendTo
// records the sender's session id on the receiver's view of the
// routed turn. Exposed as a const so callers don't have to mirror
// a magic string.
const MetaKeyFromSession = "from_session"

// Option configures a Session during Spawn or NewSession.
type Option func(*Session)

// WithProvider sets the Provider on a session.
func WithProvider(p Provider) Option {
	return func(s *Session) { s.provider = p }
}

// WithStore overrides the inherited Store.
func WithStore(st Store) Option {
	return func(s *Session) { s.store = st }
}

// WithPublisher overrides the inherited Publisher.
func WithPublisher(p Publisher) Option {
	return func(s *Session) { s.pub = p }
}

// WithRouter sets the Router for inter-session messaging.
func WithRouter(r Router) Option {
	return func(s *Session) { s.router = r }
}

// WithMetadata sets metadata on a session.
func WithMetadata(m map[string]any) Option {
	return func(s *Session) { s.Metadata = m }
}

// WithSource overrides the default Source on a session.
func WithSource(src Source) Option {
	return func(s *Session) { s.Source = src }
}

// NewSession creates a root crtx Envelope with options applied.
func NewSession(id string, opts ...Option) *Session {
	now := time.Now().UTC()
	s := &Session{
		CrtxVersion: CrtxVersion,
		ID:          id,
		CreatedAt:   now,
		UpdatedAt:   now,
		Source:      DefaultSource(),
		Turns:       []Turn{},
	}
	for _, o := range opts {
		o(s)
	}
	if s.Source.Kind == "" {
		s.Source = DefaultSource()
	}
	return s
}

// Dispatch creates a dispatch-nest child Envelope (envelope.md §7.2)
// for an open tool_call in this parent. The child:
//
//   - sets DispatchedFrom = { envelope_id: parent.ID, call_id: callID }
//   - has no parent_id / fork_point (mutually exclusive with the
//     dispatch nest signal per §7.2)
//   - inherits the parent's Store / Publisher / Router unless
//     overridden via Option
//   - starts with an empty Turns slice: dispatch nests do NOT inherit
//     parent transcript history (the curator pulls in context via
//     synthesized first turn or injected_turns)
//
// callID MUST be the call_id of an open tool_call in this parent's
// transcript at the moment of dispatch: i.e. a tool_call ContentPart
// with matching call_id whose tool_result has NOT yet been appended
// to the parent. Passing an unknown or already-closed callID is a
// programmer error and panics with a clear message — runtimes that
// build Dispatch arguments from real parent turns will never trip
// this. The matching tool_result on the parent side SHOULD set its
// child_envelope_id to the returned child's ID so consumers can walk
// from the parent transcript into the nest.
//
// Publishes crtx.session.envelope.nested on the parent's bus.
func (s *Session) Dispatch(
	ctx context.Context, id, callID string, opts ...Option,
) *Session {
	if callID == "" {
		panic("stem: Dispatch: call_id must not be empty (envelope.md §7.2)")
	}
	if !s.hasOpenToolCall(callID) {
		panic(fmt.Sprintf(
			"stem: Dispatch: call_id %q is not an open tool_call in parent %q (envelope.md §7.2)",
			callID, s.ID,
		))
	}
	now := time.Now().UTC()
	child := &Session{
		CrtxVersion: CrtxVersion,
		ID:          id,
		DispatchedFrom: &DispatchedFrom{
			EnvelopeID: s.ID,
			CallID:     callID,
		},
		CreatedAt: now,
		UpdatedAt: now,
		Source:    s.Source,
		Turns:     []Turn{},
		// Inherit parent's runtime components.
		store:  s.store,
		pub:    s.pub,
		router: s.router,
	}
	if child.Source.Kind == "" {
		child.Source = DefaultSource()
	}

	for _, o := range opts {
		o(child)
	}

	s.mu.Lock()
	s.Children = append(s.Children, id)
	s.mu.Unlock()

	if s.pub != nil {
		_ = s.pub.Publish(ctx, TopicSessionEnvelopeNested, map[string]any{
			"parent_id":  s.ID,
			"child_id":   id,
			"call_id":    callID,
			"created_at": now.Format(time.RFC3339Nano),
		})
	}

	return child
}

// hasOpenToolCall reports whether s.Turns contains a tool_call
// ContentPart with the given call_id whose matching tool_result has
// NOT yet been appended. A tool_call is "open" until any later Turn
// in the same transcript contributes a tool_result ContentPart with
// the same call_id. Already-resolved → not open. The validator
// (validate.go) accepts a re-arming sequence [call(X), result(X),
// call(X)] by deleting the call_id from openCalls on tool_result and
// re-adding it on the next tool_call; this walk mirrors that by NOT
// early-returning on the first matching tool_result — it resets
// found=false and continues so a later tool_call can re-arm.
func (s *Session) hasOpenToolCall(callID string) bool {
	if callID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	for _, t := range s.Turns {
		for _, c := range t.Content {
			switch c.Type {
			case PartTypeToolCall:
				if c.CallID == callID {
					found = true
				}
			case PartTypeToolResult:
				if c.CallID == callID {
					// Result closes the call; a later tool_call with
					// the same id may re-arm — keep walking.
					found = false
				}
			}
		}
	}
	return found
}

// ReturnDispatch folds a dispatch-nest child's result back into the
// parent's transcript: appends a tool_result Turn closing the matching
// tool_call (referencing the child via child_envelope_id) and publishes
// crtx.session.envelope.returned per events.md §3.10.
//
// callID MUST match an open tool_call in this parent's transcript (the
// one originally passed to Dispatch). child MUST be a dispatch nest of
// this parent, i.e. child.DispatchedFrom.EnvelopeID == s.ID and
// child.DispatchedFrom.CallID == callID.
//
// output is the JSON-serializable tool result; isError mirrors
// tool_result.is_error semantics (default false).
//
// Returns ErrNotDispatchChild when the child wasn't dispatched from
// this parent (or call_id doesn't match the original dispatch); returns
// ErrCallNotOpen when no matching open tool_call exists. If the parent
// has a Store, the tool_result is persisted via AppendTurnPersisted;
// otherwise it is appended in-memory.
func (s *Session) ReturnDispatch(
	ctx context.Context,
	child *Session,
	callID string,
	output any,
	isError bool,
) error {
	if child == nil || child.DispatchedFrom == nil ||
		child.DispatchedFrom.EnvelopeID != s.ID ||
		child.DispatchedFrom.CallID != callID {
		return ErrNotDispatchChild
	}

	if !s.hasOpenToolCall(callID) {
		return ErrCallNotOpen
	}

	part, err := ToolResultPart(callID, output, isError)
	if err != nil {
		return err
	}
	part.ChildEnvelopeID = child.ID

	now := time.Now().UTC()
	// Take s.mu just long enough to snapshot Turns length for the
	// sequential Turn ID. AppendTurnPersisted retakes the lock to do
	// the actual append; concurrent ReturnDispatch on the same parent
	// is not a supported pattern (a tool_call is closed exactly once).
	s.mu.Lock()
	turn := Turn{
		ID:        fmt.Sprintf("tool-%d", len(s.Turns)),
		Role:      RoleTool,
		CreatedAt: now,
		Content:   []ContentPart{part},
	}
	s.mu.Unlock()

	if err := s.AppendTurnPersisted(ctx, turn); err != nil {
		return err
	}

	if s.pub != nil {
		payload := map[string]any{
			"parent_id":  s.ID,
			"child_id":   child.ID,
			"call_id":    callID,
			"created_at": now.Format(time.RFC3339Nano),
		}
		if isError {
			payload["is_error"] = true
		}
		_ = s.pub.Publish(ctx, TopicSessionEnvelopeReturned, payload)
	}

	return nil
}

// SendTo sends a text message to another session via the Router.
// The receiving session sees the turn as RoleUser with metadata
// key [MetaKeyFromSession] set to the sender's id.
func (s *Session) SendTo(
	ctx context.Context, targetID, content string,
) error {
	if s.router == nil {
		return ErrRouterNotSet
	}

	turn := Turn{
		ID:        fmt.Sprintf("%s->%s-%d", s.ID, targetID, time.Now().UnixNano()),
		Role:      RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []ContentPart{TextPart(content)},
		Metadata: map[string]any{
			MetaKeyFromSession: s.ID,
		},
	}

	return s.router.Route(ctx, s.ID, targetID, turn)
}

// --- Router ---

// DirectRouter delivers turns by looking up sessions in a registry.
type DirectRouter struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewDirectRouter creates an empty DirectRouter.
func NewDirectRouter() *DirectRouter {
	return &DirectRouter{sessions: make(map[string]*Session)}
}

// Register adds a session to the router's registry.
func (r *DirectRouter) Register(s *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = s
}

// Unregister removes a session from the router's registry.
func (r *DirectRouter) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

// Route delivers a turn to the target session by appending it via
// the target's persisted-append so the receiver's in-memory and
// on-disk views stay consistent.
func (r *DirectRouter) Route(
	ctx context.Context, _, toID string, turn Turn,
) error {
	r.mu.RLock()
	target, ok := r.sessions[toID]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, toID)
	}

	return target.AppendTurnPersisted(ctx, turn)
}

// --- Supervisor ---

// Supervisor tracks child sessions and observes lifecycle events.
type Supervisor struct {
	mu          sync.Mutex
	children    map[string]*supervisedChild
	onChildDone func(id string)
	pub         Publisher
}

type supervisedChild struct {
	session  *Session
	cancel   context.CancelFunc
	done     chan struct{}
	doneOnce sync.Once
}

// NewSupervisor creates a Supervisor. If pub is non-nil, the
// supervisor uses it to observe child lifecycle events.
func NewSupervisor(pub Publisher) *Supervisor {
	return &Supervisor{
		children: make(map[string]*supervisedChild),
		pub:      pub,
	}
}

// OnChildDone registers a callback fired when a watched child
// session completes.
func (sv *Supervisor) OnChildDone(fn func(id string)) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.onChildDone = fn
}

// Watch registers a child session under supervision. Returns a
// derived context and cancel func for the child's lifetime.
func (sv *Supervisor) Watch(
	ctx context.Context, s *Session,
) (context.Context, context.CancelFunc, error) {
	sv.mu.Lock()
	defer sv.mu.Unlock()

	if _, exists := sv.children[s.ID]; exists {
		return nil, nil, ErrAlreadyWatched
	}

	childCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	sv.children[s.ID] = &supervisedChild{
		session: s,
		cancel:  cancel,
		done:    done,
	}

	go func() {
		select {
		case <-childCtx.Done():
		case <-done:
		}

		sv.mu.Lock()
		cb := sv.onChildDone
		sv.mu.Unlock()

		if cb != nil {
			cb(s.ID)
		}

		if sv.pub != nil {
			_ = sv.pub.Publish(context.Background(), TopicStemChildDone, map[string]any{
				"child_id": s.ID,
			})
		}
	}()

	return childCtx, cancel, nil
}

// Cancel cancels a specific child's context and signals done so
// WaitAll unblocks.
func (sv *Supervisor) Cancel(childID string) error {
	sv.mu.Lock()
	c, ok := sv.children[childID]
	sv.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrChildNotFound, childID)
	}

	c.cancel()
	c.doneOnce.Do(func() { close(c.done) })
	return nil
}

// Done signals that a child session has completed its work.
// Safe to call multiple times; only the first call closes the channel.
func (sv *Supervisor) Done(childID string) error {
	sv.mu.Lock()
	c, ok := sv.children[childID]
	sv.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrChildNotFound, childID)
	}

	c.doneOnce.Do(func() { close(c.done) })
	return nil
}

// WaitAll blocks until all watched children complete or ctx expires.
func (sv *Supervisor) WaitAll(ctx context.Context) error {
	sv.mu.Lock()
	children := make([]*supervisedChild, 0, len(sv.children))
	for _, c := range sv.children {
		children = append(children, c)
	}
	sv.mu.Unlock()

	for _, c := range children {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
		}
	}

	return nil
}
