package sqlite_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hop.top/stem"
	stemsqlite "hop.top/stem/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ctx = context.Background()

func newTestStore(t *testing.T) *stemsqlite.Store {
	t.Helper()
	s, err := stemsqlite.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCRUDRoundTrip(t *testing.T) {
	store := newTestStore(t)

	meta := stem.SessionMeta{
		ID:       "s-1",
		Source:   stem.DefaultSource(),
		Metadata: map[string]any{"model": "claude"},
	}
	require.NoError(t, store.Create(ctx, meta))

	turn := stem.Turn{
		ID:        "t-1",
		Role:      stem.RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []stem.ContentPart{stem.TextPart("hello")},
		Metadata:  map[string]any{"source": "cli"},
	}
	require.NoError(t, store.AppendTurn(ctx, "s-1", turn))

	loaded, err := store.Load(ctx, "s-1")
	require.NoError(t, err)
	assert.Equal(t, "s-1", loaded.ID)
	assert.Equal(t, stem.CrtxVersion, loaded.CrtxVersion)
	assert.Equal(t, "stem", loaded.Source.Kind)
	assert.Equal(t, "claude", loaded.Metadata["model"])
	require.Len(t, loaded.Turns, 1)
	assert.Equal(t, stem.RoleUser, loaded.Turns[0].Role)
	require.Len(t, loaded.Turns[0].Content, 1)
	assert.Equal(t, "hello", loaded.Turns[0].Content[0].Text)
	assert.Equal(t, "cli", loaded.Turns[0].Metadata["source"])
}

func TestLoadNotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Load(ctx, "nonexistent")
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestAppendTurnAutoSeq(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, store.Create(ctx, stem.SessionMeta{ID: "s-1"}))

	for i := range 5 {
		turn := stem.Turn{
			ID:        fmt.Sprintf("t-%d", i),
			Role:      stem.RoleUser,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			Content:   []stem.ContentPart{stem.TextPart(fmt.Sprintf("msg %d", i))},
		}
		require.NoError(t, store.AppendTurn(ctx, "s-1", turn))
	}

	loaded, err := store.Load(ctx, "s-1")
	require.NoError(t, err)
	require.Len(t, loaded.Turns, 5)
	for i, turn := range loaded.Turns {
		assert.Equal(t, fmt.Sprintf("t-%d", i), turn.ID)
	}
}

func TestAppendTurnToNonexistentSession(t *testing.T) {
	store := newTestStore(t)
	turn := stem.Turn{
		ID:        "t-1",
		Role:      stem.RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []stem.ContentPart{stem.TextPart("hello")},
	}
	err := store.AppendTurn(ctx, "no-such-session", turn)
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestTurnOrderingBySeq(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, store.Create(ctx, stem.SessionMeta{ID: "s-1"}))

	for i := range 3 {
		turn := stem.Turn{
			ID:        fmt.Sprintf("t-%d", i),
			Role:      stem.RoleUser,
			CreatedAt: now.Add(-time.Duration(i) * time.Second),
			Content:   []stem.ContentPart{stem.TextPart(fmt.Sprintf("msg %d", i))},
		}
		require.NoError(t, store.AppendTurn(ctx, "s-1", turn))
	}

	loaded, err := store.Load(ctx, "s-1")
	require.NoError(t, err)
	require.Len(t, loaded.Turns, 3)
	assert.Equal(t, "t-0", loaded.Turns[0].ID)
	assert.Equal(t, "t-1", loaded.Turns[1].ID)
	assert.Equal(t, "t-2", loaded.Turns[2].ID)
}

func TestListFilterByParentID(t *testing.T) {
	store := newTestStore(t)

	require.NoError(t, store.Create(ctx, stem.SessionMeta{ID: "s-1"}))
	require.NoError(t, store.Create(ctx, stem.SessionMeta{
		ID: "s-2", ParentID: "s-1",
	}))
	require.NoError(t, store.Create(ctx, stem.SessionMeta{
		ID: "s-3", ParentID: "s-1",
	}))

	children, err := store.List(ctx, stem.Filter{ParentID: "s-1"})
	require.NoError(t, err)
	assert.Len(t, children, 2)
}

func TestListLimitOffset(t *testing.T) {
	store := newTestStore(t)

	for i := range 5 {
		require.NoError(t, store.Create(ctx, stem.SessionMeta{
			ID: fmt.Sprintf("s-%d", i),
		}))
		time.Sleep(time.Millisecond)
	}

	page, err := store.List(ctx, stem.Filter{Limit: 2, Offset: 1})
	require.NoError(t, err)
	assert.Len(t, page, 2)
}

func TestListTurnCount(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, store.Create(ctx, stem.SessionMeta{ID: "s-1"}))

	for i := range 3 {
		require.NoError(t, store.AppendTurn(ctx, "s-1", stem.Turn{
			ID:        fmt.Sprintf("t-%d", i),
			Role:      stem.RoleUser,
			CreatedAt: time.Now().UTC(),
			Content:   []stem.ContentPart{stem.TextPart("msg")},
		}))
	}

	metas, err := store.List(ctx, stem.Filter{})
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, 3, metas[0].TurnCount)
}

func TestDelete(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, store.Create(ctx, stem.SessionMeta{ID: "s-del"}))
	require.NoError(t, store.AppendTurn(ctx, "s-del", stem.Turn{
		ID: "t-1", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
		Content: []stem.ContentPart{stem.TextPart("hi")},
	}))

	require.NoError(t, store.Delete(ctx, "s-del"))
	_, err := store.Load(ctx, "s-del")
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestDeleteNotFound(t *testing.T) {
	store := newTestStore(t)
	err := store.Delete(ctx, "no-such")
	assert.ErrorIs(t, err, stem.ErrSessionNotFound)
}

func TestMigrationIdempotency(t *testing.T) {
	store1, err := stemsqlite.New(":memory:")
	require.NoError(t, err)
	_ = store1.Close()

	store2, err := stemsqlite.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = store2.Close() }()

	require.NoError(t, store2.Create(ctx, stem.SessionMeta{ID: "s-idem"}))
}

func TestToolCallToolResultRoundTrip(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, store.Create(ctx, stem.SessionMeta{ID: "s-1"}))
	now := time.Now().UTC()

	tc, err := stem.ToolCallPart("c-1", "search", map[string]any{"q": "tea"})
	require.NoError(t, err)
	tr, err := stem.ToolResultPart("c-1", map[string]any{"hits": 7}, false)
	require.NoError(t, err)

	require.NoError(t, store.AppendTurn(ctx, "s-1", stem.Turn{
		ID: "a-0", Role: stem.RoleAssistant, CreatedAt: now,
		Content: []stem.ContentPart{tc},
	}))
	require.NoError(t, store.AppendTurn(ctx, "s-1", stem.Turn{
		ID: "t-0", Role: stem.RoleTool, CreatedAt: now,
		Content: []stem.ContentPart{tr},
	}))

	loaded, err := store.Load(ctx, "s-1")
	require.NoError(t, err)
	require.NoError(t, stem.Validate(loaded))
}
