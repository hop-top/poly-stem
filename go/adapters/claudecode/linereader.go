package claudecode

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrNoSession is returned by LineReader.Envelope when turns were
// mapped but no session identity was seen (no session id on any
// line, no WithSessionID, no native path to derive one from).
var ErrNoSession = errors.New("claudecode: no session identity in input")

// LineOption configures NewLineReader.
type LineOption func(*LineReader)

// WithSessionID seeds the session id used when no line carries one
// (journal records know the id before the first native line does).
// A session id on a native line takes precedence.
func WithSessionID(id string) LineOption {
	return func(p *LineReader) {
		if p.sessionID == "" {
			p.sessionID = id
		}
	}
}

// WithNativePath names the transcript file the lines come from. It
// recovers the session id from the filename when no line carries one
// and, for "agent-*" files, marks the reader as a sidechain.
func WithNativePath(path string) LineOption {
	return func(p *LineReader) {
		p.path = path
		p.account.Path = path
		p.sidechain = strings.HasPrefix(filepath.Base(path), agentFilePrefix)
		if abs, err := filepath.Abs(path); err == nil {
			p.abs = abs
		}
	}
}

func withDispatch(idx DispatchIndex) LineOption {
	return func(p *LineReader) { p.dispatch = idx }
}

// NewLineReader returns a reader ready for Feed. Stream-json lines
// (--output-format stream-json) and transcript lines are both
// accepted; see Feed for timestamp handling.
func NewLineReader(opts ...LineOption) *LineReader {
	p := &LineReader{
		account:   Account{SkippedRecords: map[string]int{}},
		refs:      DispatchIndex{},
		openCalls: map[string]struct{}{},
		userUUIDs: map[string]struct{}{},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}
