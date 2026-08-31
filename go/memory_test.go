package stem_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"hop.top/stem"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_CreateLoadRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := stem.NewMemoryStore()

	now := time.Now().UTC()
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID:        "s-1",
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{"model": "claude"},
	}))

	sess, err := s.Load(ctx, "s-1")
	require.NoError(t, err)
	assert.Equal(t, "s-1", sess.ID)
	assert.Equal(t, stem.CrtxVersion, sess.CrtxVersion)
	assert.Equal(t, "stem", sess.Source.Kind)
	assert.Empty(t, sess.Turns)
	assert.Equal(t, "claude", sess.Metadata["model"])
}

func TestMemoryStore_LoadNotFound(t *testing.T) {
	ctx := context.Background()
	s := stem.NewMemoryStore()

	_, err := s.Load(ctx, "missing")
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestMemoryStore_AppendTurnOrdering(t *testing.T) {
	ctx := context.Background()
	s := stem.NewMemoryStore()

	now := time.Now().UTC()
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID: "s-1", CreatedAt: now, UpdatedAt: now,
	}))

	for i := range 5 {
		require.NoError(t, s.AppendTurn(ctx, "s-1", stem.Turn{
			ID:        fmt.Sprintf("t-%d", i),
			Role:      stem.RoleUser,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			Content:   []stem.ContentPart{stem.TextPart(fmt.Sprintf("msg-%d", i))},
		}))
	}

	sess, err := s.Load(ctx, "s-1")
	require.NoError(t, err)
	require.Len(t, sess.Turns, 5)

	for i, turn := range sess.Turns {
		assert.Equal(t, fmt.Sprintf("t-%d", i), turn.ID)
		require.Len(t, turn.Content, 1)
		assert.Equal(t, fmt.Sprintf("msg-%d", i), turn.Content[0].Text)
	}
}

func TestMemoryStore_AppendTurnToMissing(t *testing.T) {
	ctx := context.Background()
	s := stem.NewMemoryStore()

	err := s.AppendTurn(ctx, "missing", stem.Turn{ID: "t-0"})
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestMemoryStore_ListWithFilter(t *testing.T) {
	ctx := context.Background()
	s := stem.NewMemoryStore()

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 5 {
		id := fmt.Sprintf("s-%d", i)
		require.NoError(t, s.Create(ctx, stem.SessionMeta{
			ID:        id,
			ParentID:  "root",
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
			UpdatedAt: base.Add(time.Duration(i) * time.Hour),
		}))
	}

	// Filter by parent.
	metas, err := s.List(ctx, stem.Filter{ParentID: "root"})
	require.NoError(t, err)
	assert.Len(t, metas, 5)

	// Filter by time window.
	after := base.Add(1 * time.Hour)
	before := base.Add(4 * time.Hour)
	metas, err = s.List(ctx, stem.Filter{
		After:  after,
		Before: before,
	})
	require.NoError(t, err)
	assert.Len(t, metas, 2) // s-2, s-3

	// Limit + offset.
	metas, err = s.List(ctx, stem.Filter{
		ParentID: "root",
		Limit:    2,
		Offset:   1,
	})
	require.NoError(t, err)
	assert.Len(t, metas, 2)
}

func TestMemoryStore_Delete(t *testing.T) {
	ctx := context.Background()
	s := stem.NewMemoryStore()

	now := time.Now().UTC()
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID: "s-1", CreatedAt: now, UpdatedAt: now,
	}))

	require.NoError(t, s.Delete(ctx, "s-1"))

	_, err := s.Load(ctx, "s-1")
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestMemoryStore_DeleteNotFound(t *testing.T) {
	ctx := context.Background()
	s := stem.NewMemoryStore()

	err := s.Delete(ctx, "missing")
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestMemoryStore_ConcurrentAppendTurn(t *testing.T) {
	ctx := context.Background()
	s := stem.NewMemoryStore()

	now := time.Now().UTC()
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID: "s-1", CreatedAt: now, UpdatedAt: now,
	}))

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func(idx int) {
			defer wg.Done()
			require.NoError(t, s.AppendTurn(ctx, "s-1", stem.Turn{
				ID:        fmt.Sprintf("t-%d", idx),
				Role:      stem.RoleUser,
				CreatedAt: time.Now().UTC(),
				Content:   []stem.ContentPart{stem.TextPart("concurrent")},
			}))
		}(i)
	}
	wg.Wait()

	sess, err := s.Load(ctx, "s-1")
	require.NoError(t, err)
	assert.Len(t, sess.Turns, n)
}

func TestMemoryStore_AppendTurnClosedSession(t *testing.T) {
	ctx := context.Background()
	s := stem.NewMemoryStore()

	now := time.Now().UTC()
	require.NoError(t, s.Create(ctx, stem.SessionMeta{
		ID: "s-1", CreatedAt: now, UpdatedAt: now,
	}))

	require.NoError(t, s.AppendTurn(ctx, "s-1", stem.Turn{
		ID: "t-0", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
		Content: []stem.ContentPart{stem.TextPart("open")},
	}))

	require.NoError(t, s.CloseSession(ctx, "s-1"))

	err := s.AppendTurn(ctx, "s-1", stem.Turn{
		ID: "t-1", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
		Content: []stem.ContentPart{stem.TextPart("closed")},
	})
	assert.ErrorIs(t, err, stem.ErrSessionClosed)
}
