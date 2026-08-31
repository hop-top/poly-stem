package sessions

import (
	"iter"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stem "hop.top/stem"
	"hop.top/stem/adapters"
)

// stubAdapter yields canned results on the shared seam.
type stubAdapter struct {
	kind    string
	results []*adapters.Result
}

func (s stubAdapter) Kind() string           { return s.kind }
func (s stubAdapter) DefaultRoots() []string { return nil }
func (s stubAdapter) Scan(string) iter.Seq2[*adapters.Result, error] {
	return func(yield func(*adapters.Result, error) bool) {
		for _, r := range s.results {
			if !yield(r, nil) {
				return
			}
		}
	}
}

func stubEnv(id, kind, updated string) *stem.Session {
	env := &stem.Session{
		CrtxVersion: "0.1",
		ID:          id,
		Source:      stem.Source{Kind: kind, Version: "1.0.0"},
		Metadata:    map[string]any{adapters.MetaNativePath: "/native/" + id},
	}
	env.CreatedAt, _ = time.Parse(time.RFC3339, updated)
	env.UpdatedAt = env.CreatedAt
	return env
}

func writeCrtxFile(t *testing.T, dir, id, updated string) string {
	t.Helper()
	path := filepath.Join(dir, id+".jsonl")
	doc := `{"crtx_version":"0.1","id":"` + id + `","created_at":"` + updated +
		`","updated_at":"` + updated +
		`","source":{"kind":"stem","version":"0.1.0"},"turns":[{"id":"t1","role":"user","created_at":"` +
		updated + `","content":[{"type":"text","text":"hello"}]}]}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(doc), 0o644))
	return path
}

func TestScanDedupCrtxNativeWinsOverAdapter(t *testing.T) {
	const id = "0198f2d4-aaaa-7000-8000-000000000001"
	dir := t.TempDir()
	writeCrtxFile(t, dir, id, "2026-08-01T00:00:00Z")

	// The adapter-normalized duplicate is more recently updated —
	// class preference must still hand the session to the crtx root.
	stub := stubAdapter{kind: "claude-code", results: []*adapters.Result{
		{Envelope: stubEnv(id, "claude-code", "2026-08-07T00:00:00Z")},
	}}

	recs := Scan(Options{
		CrtxRoots: []string{dir},
		Sources:   []Source{{Adapter: stub, Roots: []string{"unused"}}},
	})
	require.Len(t, recs, 1)
	assert.Equal(t, "stem", recs[0].Envelope.Source.Kind,
		"crtx-native must beat adapter-normalized regardless of updated_at")
	assert.Equal(t, "crtx", recs[0].Store.Adapter)
	assert.NotNil(t, recs[0].Raw)
}

func TestScanDedupNewestWinsWithinClass(t *testing.T) {
	const id = "0198f2d4-bbbb-7000-8000-000000000002"
	older := stubAdapter{kind: "codex", results: []*adapters.Result{
		{Envelope: stubEnv(id, "codex", "2026-08-01T00:00:00Z")},
	}}
	newer := stubAdapter{kind: "claude-code", results: []*adapters.Result{
		{Envelope: stubEnv(id, "claude-code", "2026-08-05T00:00:00Z")},
	}}

	recs := Scan(Options{Sources: []Source{
		{Adapter: older, Roots: []string{"a"}},
		{Adapter: newer, Roots: []string{"b"}},
	}})
	require.Len(t, recs, 1)
	assert.Equal(t, "claude-code", recs[0].Envelope.Source.Kind,
		"most recently updated duplicate wins within a class")

	// Same set, reversed discovery order: outcome must not change.
	recs = Scan(Options{Sources: []Source{
		{Adapter: newer, Roots: []string{"b"}},
		{Adapter: older, Roots: []string{"a"}},
	}})
	require.Len(t, recs, 1)
	assert.Equal(t, "claude-code", recs[0].Envelope.Source.Kind)
}

func TestScanDedupNewestCrtxNativeWins(t *testing.T) {
	const id = "0198f2d4-cccc-7000-8000-000000000003"
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeCrtxFile(t, dirA, id, "2026-08-01T00:00:00Z")
	newest := writeCrtxFile(t, dirB, id, "2026-08-06T00:00:00Z")

	recs := Scan(Options{CrtxRoots: []string{dirA, dirB}})
	require.Len(t, recs, 1)
	assert.Equal(t, newest, recs[0].Store.Path,
		"most recently updated crtx-native duplicate wins")
}
