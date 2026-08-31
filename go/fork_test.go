package stem_test

import (
	"encoding/json"
	"testing"
	"time"

	"hop.top/stem"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkCreatesEnvelopeWithParentAndForkPoint(t *testing.T) {
	parent := stem.NewSession("parent-1")
	parent.Turns = []stem.Turn{
		{ID: "t-0", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{stem.TextPart("a")}},
		{ID: "t-1", Role: stem.RoleAssistant, CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{stem.TextPart("b")}},
	}

	forked := parent.Fork("child-1")

	assert.Equal(t, "child-1", forked.ID)
	assert.Equal(t, "parent-1", forked.ParentID)
	require.NotNil(t, forked.ForkPoint)
	assert.Equal(t, 2, *forked.ForkPoint)
	assert.Empty(t, forked.Turns, "child carries only post-fork additions")
	assert.NotZero(t, forked.CreatedAt)
}

func TestForkIndependence(t *testing.T) {
	parent := stem.NewSession("p-1",
		stem.WithMetadata(map[string]any{"k": "v"}))
	parent.Turns = []stem.Turn{
		{ID: "t-1", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{stem.TextPart("msg1")}},
	}

	forked := parent.Fork("f-1")

	// Append to fork.
	forked.Turns = append(forked.Turns, stem.Turn{
		ID: "t-new", Role: stem.RoleAssistant, CreatedAt: time.Now().UTC(),
		Content: []stem.ContentPart{stem.TextPart("reply")},
	})

	// Append to parent.
	parent.Turns = append(parent.Turns, stem.Turn{
		ID: "t-p2", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
		Content: []stem.ContentPart{stem.TextPart("msg2")},
	})

	// They remain independent.
	assert.Len(t, parent.Turns, 2)
	assert.Len(t, forked.Turns, 1)
	assert.Equal(t, "t-p2", parent.Turns[1].ID)
	assert.Equal(t, "t-new", forked.Turns[0].ID)

	// Metadata independence.
	forked.Metadata["k"] = "changed"
	assert.Equal(t, "v", parent.Metadata["k"])
}

func TestForkEmptySession(t *testing.T) {
	parent := stem.NewSession("p-empty")
	forked := parent.Fork("f-empty")
	assert.Equal(t, "p-empty", forked.ParentID)
	require.NotNil(t, forked.ForkPoint)
	assert.Equal(t, 0, *forked.ForkPoint)
	assert.Empty(t, forked.Turns)
}

func TestForkJSONLRoundTrip(t *testing.T) {
	parent := stem.NewSession("parent-1")
	parent.Turns = []stem.Turn{
		{ID: "t-0", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{stem.TextPart("hello")}},
		{ID: "t-1", Role: stem.RoleAssistant, CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{stem.TextPart("hi")}},
	}

	child := parent.Fork("child-1")
	child.Turns = append(child.Turns, stem.Turn{
		ID: "c-0", Role: stem.RoleAssistant, CreatedAt: time.Now().UTC(),
		Content: []stem.ContentPart{stem.TextPart("alternate take")},
	})

	// Round-trip each through JSON.
	for _, env := range []*stem.Session{parent, child} {
		data, err := json.Marshal(env)
		require.NoError(t, err)
		var got stem.Session
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, env.ID, got.ID)
		assert.Equal(t, env.ParentID, got.ParentID)
		if env.ForkPoint != nil {
			require.NotNil(t, got.ForkPoint)
			assert.Equal(t, *env.ForkPoint, *got.ForkPoint)
		}
		require.NoError(t, stem.Validate(&got))
	}
}
