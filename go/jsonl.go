package stem

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// JSONLStore persists sessions as one JSONL file per session, where
// each line is a complete crtx Envelope. v0.1 writes exactly one
// envelope (one line) per file and rewrites the whole file on every
// append. Producers that need true streaming append should use a
// different backend.
//
// The line format keeps the door open for future formats that
// stream multiple envelopes per file (e.g. session-per-line "index"
// files) without changing the loader.
//
// Concurrency: single-process safe. AppendTurn serializes
// read-modify-write per session id via an internal mutex map. Cross-
// process safety requires external file locking (not provided here).
type JSONLStore struct {
	dir string

	// locks holds per-session write mutexes for serializing AppendTurn
	// under a single process. NOTE: entries are not evicted; for
	// long-lived processes that churn through many distinct session
	// ids, this grows O(N). A future release may add TTL-based
	// eviction. Cross-process safety requires external locking; see
	// JSONLStore doc.
	locks sync.Map // map[string]*sync.Mutex
}

// NewJSONLStore returns a store rooted at dir. The directory is
// created on the first write if it does not exist.
func NewJSONLStore(dir string) *JSONLStore {
	return &JSONLStore{dir: dir}
}

func (s *JSONLStore) path(id string) string {
	return filepath.Join(s.dir, id+".jsonl")
}

func (s *JSONLStore) ensureDir() error {
	return os.MkdirAll(s.dir, 0o755)
}

// Create initializes a session file with an empty Turns envelope.
func (s *JSONLStore) Create(
	_ context.Context, meta SessionMeta,
) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	if meta.Source.Kind == "" {
		meta.Source = DefaultSource()
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.CreatedAt
	}

	env := &Session{
		CrtxVersion: CrtxVersion,
		ID:          meta.ID,
		CreatedAt:   meta.CreatedAt,
		UpdatedAt:   meta.UpdatedAt,
		Source:      meta.Source,
		ParentID:    meta.ParentID,
		ForkPoint:   meta.ForkPoint,
		Metadata:    deepCopyMeta(meta.Metadata),
		Turns:       []Turn{},
	}
	return writeEnvelopeLine(s.path(meta.ID), env)
}

// Load reads a session file and returns the latest envelope.
func (s *JSONLStore) Load(
	_ context.Context, id string,
) (*Session, error) {
	env, err := readEnvelopeLine(s.path(id))
	if err != nil {
		return nil, err
	}
	if err := Validate(env); err != nil {
		return nil, fmt.Errorf("stem: jsonl load: %w", err)
	}
	return env, nil
}

// AppendTurn rewrites the session file with the new turn appended.
// Read-modify-write is serialized per session id by an in-process
// mutex so concurrent appenders do not lose turns.
func (s *JSONLStore) AppendTurn(
	_ context.Context, sessionID string, turn Turn,
) error {
	mu := s.sessionLock(sessionID)
	mu.Lock()
	defer mu.Unlock()

	env, err := readEnvelopeLine(s.path(sessionID))
	if err != nil {
		return err
	}
	env.Turns = append(env.Turns, deepCopyTurn(turn))
	env.UpdatedAt = time.Now().UTC()
	return writeEnvelopeLine(s.path(sessionID), env)
}

func (s *JSONLStore) sessionLock(id string) *sync.Mutex {
	if v, ok := s.locks.Load(id); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := s.locks.LoadOrStore(id, mu)
	return actual.(*sync.Mutex)
}

// List enumerates session metadata from the directory's .jsonl files.
func (s *JSONLStore) List(
	_ context.Context, f Filter,
) ([]SessionMeta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []SessionMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}

		env, err := readEnvelopeLine(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue // skip corrupt files
		}

		meta := SessionMeta{
			ID:        env.ID,
			TurnCount: len(env.Turns),
			CreatedAt: env.CreatedAt,
			UpdatedAt: env.UpdatedAt,
			ParentID:  env.ParentID,
			ForkPoint: env.ForkPoint,
			Source:    env.Source,
			Metadata:  deepCopyMeta(env.Metadata),
		}

		if f.ParentID != "" && meta.ParentID != f.ParentID {
			continue
		}
		if !f.After.IsZero() && !meta.CreatedAt.After(f.After) {
			continue
		}
		if !f.Before.IsZero() && !meta.CreatedAt.Before(f.Before) {
			continue
		}

		out = append(out, meta)
	}

	if f.Offset > 0 && f.Offset < len(out) {
		out = out[f.Offset:]
	} else if f.Offset >= len(out) && f.Offset > 0 {
		return nil, nil
	}

	if f.Limit > 0 && f.Limit < len(out) {
		out = out[:f.Limit]
	}

	return out, nil
}

// Delete removes a session file.
func (s *JSONLStore) Delete(_ context.Context, id string) error {
	p := s.path(id)
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return ErrSessionNotFound
		}
		return err
	}
	return os.Remove(p)
}

// ListTurns returns turns for the given session matching the filter.
func (s *JSONLStore) ListTurns(ctx context.Context, sessionID string, f TurnFilter) ([]Turn, error) {
	sess, err := s.Load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var result []Turn
	for _, t := range sess.Turns {
		if f.Role != "" && t.Role != f.Role {
			continue
		}
		if !f.After.IsZero() && !t.CreatedAt.After(f.After) {
			continue
		}
		if !f.Before.IsZero() && !t.CreatedAt.Before(f.Before) {
			continue
		}
		result = append(result, t)
	}
	if f.Offset > 0 && f.Offset < len(result) {
		result = result[f.Offset:]
	} else if f.Offset >= len(result) {
		return nil, nil
	}
	if f.Limit > 0 && f.Limit < len(result) {
		result = result[:f.Limit]
	}
	return result, nil
}

func (s *JSONLStore) Close() error { return nil }

// --- file I/O helpers ---

func readEnvelopeLine(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReader(f)
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	line = trimNewline(line)
	if len(line) == 0 {
		return nil, ErrSessionNotFound
	}
	var env Session
	if err := json.Unmarshal(line, &env); err != nil {
		return nil, fmt.Errorf("stem: jsonl unmarshal: %w", err)
	}
	return &env, nil
}

func writeEnvelopeLine(path string, env *Session) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("stem: jsonl marshal: %w", err)
	}
	return fsyncedWriteAndRename(path, append(data, '\n'))
}

// fsyncedWriteAndRename writes data to a sibling tmp file, fsyncs
// both the file and its parent directory, then renames into place.
// Provides durability against power loss within the limits of the
// host filesystem.
func fsyncedWriteAndRename(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp := target + ".tmp"

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	// Fsync the directory to persist the rename. Best-effort: some
	// filesystems / OSes return errors here that are not actionable
	// (e.g. Windows). Surface only the open error.
	d, err := os.Open(dir)
	if err != nil {
		return nil
	}
	_ = d.Sync()
	_ = d.Close()
	return nil
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// compile-time interface checks
var _ Store = (*JSONLStore)(nil)
var _ Store = (*MemoryStore)(nil)
