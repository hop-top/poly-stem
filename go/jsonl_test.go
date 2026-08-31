package stem_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hop.top/stem"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newJSONLStore(t *testing.T) (*stem.JSONLStore, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sessions")
	return stem.NewJSONLStore(dir), dir
}

func TestJSONLStore_CreateWritesEnvelopeLine(t *testing.T) {
	ctx := context.Background()
	s, dir := newJSONLStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID:        "s-1",
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{"model": "opus"},
	}))

	data, err := os.ReadFile(filepath.Join(dir, "s-1.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"crtx_version":"0.1"`)
	assert.Contains(t, string(data), `"id":"s-1"`)
	assert.Contains(t, string(data), `"opus"`)
	// One envelope = one line.
	assert.Equal(t, 1, strings.Count(strings.TrimRight(string(data), "\n"), "\n")+1)
}

func TestJSONLStore_AppendRewritesEnvelope(t *testing.T) {
	ctx := context.Background()
	s, dir := newJSONLStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID: "s-1", CreatedAt: now, UpdatedAt: now,
	}))

	for i := range 3 {
		require.NoError(t, s.AppendTurn(ctx, "s-1", stem.Turn{
			ID:        fmt.Sprintf("t-%d", i),
			Role:      stem.RoleUser,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			Content:   []stem.ContentPart{stem.TextPart(fmt.Sprintf("msg-%d", i))},
		}))
	}

	data, err := os.ReadFile(filepath.Join(dir, "s-1.jsonl"))
	require.NoError(t, err)
	// Still one line (envelope-per-line v0.1 layout).
	trimmed := strings.TrimRight(string(data), "\n")
	assert.NotContains(t, trimmed, "\n")

	var env stem.Session
	require.NoError(t, json.Unmarshal([]byte(trimmed), &env))
	require.Len(t, env.Turns, 3)
}

func TestJSONLStore_LoadReconstructsEnvelope(t *testing.T) {
	ctx := context.Background()
	s, _ := newJSONLStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	forkPoint := 0
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID:        "s-1",
		ParentID:  "root",
		ForkPoint: &forkPoint,
		CreatedAt: now,
		UpdatedAt: now,
	}))

	for i := range 3 {
		require.NoError(t, s.AppendTurn(ctx, "s-1", stem.Turn{
			ID:        fmt.Sprintf("t-%d", i),
			Role:      stem.RoleAssistant,
			Content:   []stem.ContentPart{stem.TextPart(fmt.Sprintf("resp-%d", i))},
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}))
	}

	sess, err := s.Load(ctx, "s-1")
	require.NoError(t, err)
	assert.Equal(t, "s-1", sess.ID)
	assert.Equal(t, "root", sess.ParentID)
	require.NotNil(t, sess.ForkPoint)
	require.Len(t, sess.Turns, 3)
	assert.Equal(t, "resp-0", sess.Turns[0].Content[0].Text)
	assert.Equal(t, "resp-2", sess.Turns[2].Content[0].Text)
}

func TestJSONLStore_LoadNotFound(t *testing.T) {
	ctx := context.Background()
	s, _ := newJSONLStore(t)

	_, err := s.Load(ctx, "missing")
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestJSONLStore_ListReadsMetadata(t *testing.T) {
	ctx := context.Background()
	s, _ := newJSONLStore(t)

	base := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	forkPoint := 0
	for i := range 3 {
		id := fmt.Sprintf("s-%d", i)
		require.NoError(t, s.Create(ctx, stem.SessionMeta{
			ID:        id,
			ParentID:  "root",
			ForkPoint: &forkPoint,
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
			UpdatedAt: base.Add(time.Duration(i) * time.Hour),
		}))
		if i == 1 {
			require.NoError(t, s.AppendTurn(ctx, id, stem.Turn{
				ID: "t-0", Role: stem.RoleUser, CreatedAt: base,
				Content: []stem.ContentPart{stem.TextPart("hi")},
			}))
		}
	}

	metas, err := s.List(ctx, stem.Filter{ParentID: "root"})
	require.NoError(t, err)
	assert.Len(t, metas, 3)

	for _, m := range metas {
		if m.ID == "s-1" {
			assert.Equal(t, 1, m.TurnCount)
		}
	}
}

func TestJSONLStore_FilePerSessionIsolation(t *testing.T) {
	ctx := context.Background()
	s, _ := newJSONLStore(t)

	now := time.Now().UTC()
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID: "a", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID: "b", CreatedAt: now, UpdatedAt: now,
	}))

	require.NoError(t, s.AppendTurn(ctx, "a", stem.Turn{
		ID: "t-a", Role: stem.RoleUser, CreatedAt: now,
		Content: []stem.ContentPart{stem.TextPart("only-in-a")},
	}))

	sessA, err := s.Load(ctx, "a")
	require.NoError(t, err)
	assert.Len(t, sessA.Turns, 1)

	sessB, err := s.Load(ctx, "b")
	require.NoError(t, err)
	assert.Empty(t, sessB.Turns)
}

func TestJSONLStore_Delete(t *testing.T) {
	ctx := context.Background()
	s, _ := newJSONLStore(t)

	now := time.Now().UTC()
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID: "s-1", CreatedAt: now, UpdatedAt: now,
	}))

	require.NoError(t, s.Delete(ctx, "s-1"))

	_, err := s.Load(ctx, "s-1")
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestJSONLStore_DeleteNotFound(t *testing.T) {
	ctx := context.Background()
	s, _ := newJSONLStore(t)

	err := s.Delete(ctx, "missing")
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestJSONLStore_AppendToMissing(t *testing.T) {
	ctx := context.Background()
	s, _ := newJSONLStore(t)

	err := s.AppendTurn(ctx, "missing", stem.Turn{ID: "t-0"})
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

// TestJSONLStore_ConcurrentAppendTurn fires N goroutines at the same
// session id. Without the per-session mutex the read-modify-write
// cycle drops turns under last-writer-wins. With the mutex, all N
// turns appear in the final envelope.
func TestJSONLStore_ConcurrentAppendTurn(t *testing.T) {
	ctx := context.Background()
	s, _ := newJSONLStore(t)

	now := time.Now().UTC()
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID: "s-1", CreatedAt: now, UpdatedAt: now,
	}))

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			err := s.AppendTurn(ctx, "s-1", stem.Turn{
				ID:        fmt.Sprintf("t-%d", idx),
				Role:      stem.RoleUser,
				CreatedAt: now.Add(time.Duration(idx) * time.Millisecond),
				Content:   []stem.ContentPart{stem.TextPart(fmt.Sprintf("msg-%d", idx))},
			})
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()

	sess, err := s.Load(ctx, "s-1")
	require.NoError(t, err)
	assert.Len(t, sess.Turns, n)
}
