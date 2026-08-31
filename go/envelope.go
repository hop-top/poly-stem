// Package stem provides the Go SDK for stem, the polyglot AI agent
// runtime. Envelopes produced by stem follow the crtx v0.1 spec:
// https://spec.hop.top/crtx/v0.1/envelope.schema.json
package stem

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Sentinel errors.
var (
	ErrSessionNotFound = errors.New("stem: session not found")
	ErrSessionClosed   = errors.New("stem: session is closed")
)

// Role identifies the author of a turn. Follows crtx v0.1 enum.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
)

// Source identifies the runtime that produced the Envelope.
// kind + version are required by crtx; instance is optional.
type Source struct {
	Kind     string `json:"kind"`
	Version  string `json:"version"`
	Instance string `json:"instance,omitempty"`
}

// DefaultSource returns the canonical stem Source: kind=stem,
// version=stem semver.
func DefaultSource() Source {
	return Source{Kind: SourceKind, Version: Version}
}

// Turn is one contribution within a Session/Envelope. Ordered by
// position in Session.Turns, not by CreatedAt.
//
// AgentID and InReplyToCallID are crtx v0.1 multi-agent provenance
// fields. See envelope.md §3.1 and §3.2 in the spec.
type Turn struct {
	ID              string         `json:"id"`
	Role            Role           `json:"role"`
	CreatedAt       time.Time      `json:"created_at"`
	Content         []ContentPart  `json:"content"`
	AgentID         string         `json:"agent_id,omitempty"`
	InReplyToCallID string         `json:"in_reply_to_call_id,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// DispatchedFrom marks an Envelope as a dispatch nest spawned by an
// open tool_call in another Envelope. Both members are required by
// the schema; mutually exclusive with ParentID/ForkPoint. See
// envelope.md §7.2.
type DispatchedFrom struct {
	EnvelopeID string `json:"envelope_id"`
	CallID     string `json:"call_id"`
}

// InjectedTurnRef references a slice of Turns in another Envelope to
// be included as part of this Envelope's context. Append-only. See
// envelope.md §7.3.
type InjectedTurnRef struct {
	EnvelopeID          string `json:"envelope_id"`
	StartTurnID         string `json:"start_turn_id"`
	EndTurnID           string `json:"end_turn_id"`
	InjectedAfterTurnID string `json:"injected_after_turn_id,omitempty"`
}

// Session is a stem-shaped crtx Envelope plus runtime handles.
// JSON-encoded Session matches the crtx Envelope schema exactly.
type Session struct {
	CrtxVersion    string            `json:"crtx_version"`
	ID             string            `json:"id"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	Source         Source            `json:"source"`
	Turns          []Turn            `json:"turns"`
	ParentID       string            `json:"parent_id,omitempty"`
	ForkPoint      *int              `json:"fork_point,omitempty"`
	DispatchedFrom *DispatchedFrom   `json:"dispatched_from,omitempty"`
	InjectedTurns  []InjectedTurnRef `json:"injected_turns,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`

	// Runtime (unexported, not serialized).
	mu       sync.Mutex `json:"-"`
	provider Provider   `json:"-"`
	store    Store      `json:"-"`
	pub      Publisher  `json:"-"`
	router   Router     `json:"-"`

	// Children is not on the crtx wire; tracked in memory for Spawn
	// supervision.
	Children []string `json:"-"`

	// ClosedAt is not on the crtx wire; used by stores to enforce
	// append-only semantics within a process.
	ClosedAt *time.Time `json:"-"`
}

// SessionMeta is a lightweight summary for listing without loading
// full turn data. Stem-internal: not part of crtx wire.
//
// ParentID + ForkPoint follow crtx semantics: both must be set
// together if either is set, and both mean "this Envelope is a fork
// of parent_id at parent.turns[fork_point]".
type SessionMeta struct {
	ID        string         `json:"id"`
	TurnCount int            `json:"turn_count"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	ParentID  string         `json:"parent_id,omitempty"`
	ForkPoint *int           `json:"fork_point,omitempty"`
	Source    Source         `json:"source,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Filter constrains session listing queries.
type Filter struct {
	Before   time.Time `json:"before,omitempty"`
	After    time.Time `json:"after,omitempty"`
	ParentID string    `json:"parent_id,omitempty"`
	Limit    int       `json:"limit,omitempty"`
	Offset   int       `json:"offset,omitempty"`
}

// TurnFilter constrains turn listing queries.
type TurnFilter struct {
	Role   Role      `json:"role,omitempty"`
	After  time.Time `json:"after,omitempty"`
	Before time.Time `json:"before,omitempty"`
	Limit  int       `json:"limit,omitempty"`
	Offset int       `json:"offset,omitempty"`
}

// Router delivers turns between sessions.
type Router interface {
	Route(ctx context.Context, fromID, toID string, turn Turn) error
}

// Store is the persistence interface for sessions and turns.
//
// List ordering is implementation-defined: MemoryStore returns oldest
// first (ASC). Use Filter.Limit/Offset for pagination.
type Store interface {
	Create(ctx context.Context, meta SessionMeta) error
	Load(ctx context.Context, id string) (*Session, error)
	AppendTurn(ctx context.Context, sessionID string, turn Turn) error
	List(ctx context.Context, f Filter) ([]SessionMeta, error)
	ListTurns(ctx context.Context, sessionID string, f TurnFilter) ([]Turn, error)
	Delete(ctx context.Context, id string) error
	Close() error
}

// Fork creates a child Envelope rooted at the parent at the given
// ForkPoint (number of parent turns to inherit conceptually). The
// child starts with an empty Turns slice per crtx semantics: a fork's
// turns array carries only post-fork additions; consumers reconstruct
// by concatenating parent.Turns[0:ForkPoint] with child.Turns.
func (s *Session) Fork(id string) *Session {
	now := time.Now().UTC()
	fp := len(s.Turns)
	src := s.Source
	if src.Kind == "" {
		src = DefaultSource()
	}
	return &Session{
		CrtxVersion: CrtxVersion,
		ID:          id,
		CreatedAt:   now,
		UpdatedAt:   now,
		Source:      src,
		Turns:       []Turn{},
		ParentID:    s.ID,
		ForkPoint:   &fp,
		Metadata:    deepCopyMeta(s.Metadata),
	}
}

func deepCopyTurn(t Turn) Turn {
	cp := t
	if t.Content != nil {
		cp.Content = make([]ContentPart, len(t.Content))
		for i, p := range t.Content {
			cp.Content[i] = p.clone()
		}
	}
	if t.Metadata != nil {
		cp.Metadata = make(map[string]any, len(t.Metadata))
		for k, v := range t.Metadata {
			cp.Metadata[k] = v
		}
	}
	return cp
}

func deepCopyMeta(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// MarshalIndent is a convenience for callers that want a canonical
// crtx JSON rendering of a Session.
func (s *Session) MarshalIndent(prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(s, prefix, indent)
}

// AppendTurnPersisted appends a turn to the session's in-memory
// turns slice and, if a Store is configured, persists it through
// Store.AppendTurn. The in-memory mutation and the persistence call
// run under the session's mutex so concurrent appenders see a
// consistent view.
//
// On store failure the in-memory append is rolled back so the
// in-memory transcript never leads disk. Callers receive the
// underlying store error verbatim and may retry without observing a
// half-applied state.
//
// A nil Store is treated as in-memory-only; not an error.
func (s *Session) AppendTurnPersisted(ctx context.Context, t Turn) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prevUpdatedAt := s.UpdatedAt
	s.Turns = append(s.Turns, t)
	s.UpdatedAt = time.Now().UTC()

	if s.store == nil {
		return nil
	}
	if err := s.store.AppendTurn(ctx, s.ID, t); err != nil {
		// Roll back the in-memory mutation so the caller observes an
		// atomic outcome: either both in-memory and store advance, or
		// neither does.
		s.Turns = s.Turns[:len(s.Turns)-1]
		s.UpdatedAt = prevUpdatedAt
		return err
	}
	return nil
}
