// Package projection turns a journal of native harness lines into
// crtx envelopes. A journal record is one verbatim native line (a
// Claude Code stream-json line, a Codex app-server JSON-RPC message)
// plus the metadata the journal owns: session id, sequence number,
// receive time. A Projector feeds records, in order, to the matching
// adapter line reader and serves the result as a full envelope or as
// the turns after a cursor (an `/envelope?after=<seq>` endpoint).
//
// Resume model: Position is the seq of the last applied record.
// Rebuilding after a restart replays the journal into a fresh
// Projector; Apply is O(1) per record. Since(seq) pages the turn
// list by journal seq so a reader that already holds turns up to seq
// asks for the rest.
package projection

import (
	"errors"
	"fmt"
	"iter"
	"time"

	stem "hop.top/stem"
	"hop.top/stem/adapters"
	"hop.top/stem/adapters/claudecode"
	"hop.top/stem/adapters/codex"
)

var (
	// ErrUnknownKind is returned for a harness kind with no adapter.
	ErrUnknownKind = errors.New("projection: unknown harness kind")
	// ErrOutOfOrder is returned when a record's seq does not exceed
	// the last applied seq (duplicate or regression). State is left
	// untouched.
	ErrOutOfOrder = errors.New("projection: record out of order")
)

// Record is one journal entry.
type Record struct {
	// SessionID is the journal's session id. It seeds envelope
	// identity; a session id carried by the native lines wins.
	SessionID string
	// Seq is the journal sequence number; strictly increasing, gaps
	// allowed (the journal may hold non-harness records).
	Seq uint64
	// At is when the journal received the line; stamps turns whose
	// native line carries no timestamp.
	At time.Time
	// Line is the native line, verbatim.
	Line []byte
}

// Projector projects one session's records. Not safe for
// concurrent use.
type Projector struct {
	reader adapters.LineReader
	kind   string
	pos    uint64
	turns  []stem.Turn
	seqs   []uint64 // seqs[i] = seq of the record that produced turns[i]
}

// New returns a Projector for a harness kind: the crtx source.kind
// of the adapter ("claude-code" or "codex"). The first record's
// SessionID seeds envelope identity.
func New(kind string) (*Projector, error) {
	p := &Projector{kind: kind}
	switch kind {
	case claudecode.SourceKind:
		p.reader = claudecode.NewLineReader()
	case codex.SourceKind:
		p.reader = codex.NewLineReader()
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	return p, nil
}

// Project applies every record from recs and returns the Projector.
func Project(kind string, recs []Record) (*Projector, error) {
	p, err := New(kind)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		if _, err := p.Apply(r); err != nil {
			return p, err
		}
	}
	return p, nil
}

// ProjectSeq is Project over an iterator.
func ProjectSeq(kind string, recs iter.Seq[Record]) (*Projector, error) {
	p, err := New(kind)
	if err != nil {
		return nil, err
	}
	for r := range recs {
		if _, err := p.Apply(r); err != nil {
			return p, err
		}
	}
	return p, nil
}

// Apply feeds one record and returns the turns it completed.
func (p *Projector) Apply(rec Record) ([]stem.Turn, error) {
	if rec.Seq <= p.pos {
		return nil, fmt.Errorf("%w: seq %d after %d", ErrOutOfOrder, rec.Seq, p.pos)
	}
	p.seed(rec.SessionID)
	got := p.reader.Feed(rec.Line, rec.At)
	p.pos = rec.Seq
	for _, t := range got {
		p.turns = append(p.turns, t)
		p.seqs = append(p.seqs, rec.Seq)
	}
	return got, nil
}

// seed hands the journal session id to the reader before its first
// native line is parsed. Readers keep native identity over the seed.
func (p *Projector) seed(id string) {
	if id == "" || p.pos != 0 {
		return
	}
	switch p.kind {
	case claudecode.SourceKind:
		p.reader = claudecode.NewLineReader(claudecode.WithSessionID(id))
	case codex.SourceKind:
		p.reader = codex.NewLineReader(codex.WithSessionID(id))
	}
}

// Position is the seq of the last applied record; 0 before any.
func (p *Projector) Position() uint64 { return p.pos }

// Envelope returns the full envelope for the records applied so far.
// Nil, nil while no turn has been produced (claude-code); codex
// returns its ErrNoSession until identity is known.
func (p *Projector) Envelope() (*stem.Session, error) { return p.reader.Envelope() }

// Since returns the turns produced by records with seq > after, in
// order. Since(0) is every turn. The slice is a copy.
func (p *Projector) Since(after uint64) []stem.Turn {
	i := 0
	for i < len(p.seqs) && p.seqs[i] <= after {
		i++
	}
	return append([]stem.Turn(nil), p.turns[i:]...)
}

// Account reports what the adapter dropped so far.
func (p *Projector) Account() adapters.Accounting { return p.reader.Account() }
