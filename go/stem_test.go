package stem_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hop.top/stem"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tc, err := stem.ToolCallPart("tc-1", "read", map[string]any{"path": "foo"})
	require.NoError(t, err)
	turn := stem.Turn{
		ID:        "t-1",
		Role:      stem.RoleAssistant,
		CreatedAt: now,
		Metadata:  map[string]any{"key": "val"},
		Content: []stem.ContentPart{
			stem.TextPart("hello"),
			tc,
		},
	}

	data, err := json.Marshal(turn)
	require.NoError(t, err)

	var got stem.Turn
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, turn.ID, got.ID)
	assert.Equal(t, turn.Role, got.Role)
	assert.Equal(t, turn.CreatedAt.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, turn.Metadata, got.Metadata)
	require.Len(t, got.Content, 2)
	assert.Equal(t, stem.PartTypeText, got.Content[0].Type)
	assert.Equal(t, "hello", got.Content[0].Text)
	assert.Equal(t, stem.PartTypeToolCall, got.Content[1].Type)
	assert.Equal(t, "tc-1", got.Content[1].CallID)
	assert.Equal(t, "read", got.Content[1].Name)
	assert.JSONEq(t, `{"path":"foo"}`, string(got.Content[1].Input))
}

func TestSessionEnvelopeShape(t *testing.T) {
	s := stem.NewSession("s-1")
	s.Turns = append(s.Turns, stem.Turn{
		ID:        "t-0",
		Role:      stem.RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []stem.ContentPart{stem.TextPart("hi")},
	})

	data, err := json.Marshal(s)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "0.1", raw["crtx_version"])
	assert.Equal(t, "s-1", raw["id"])
	src := raw["source"].(map[string]any)
	assert.Equal(t, "stem", src["kind"])
	assert.Equal(t, "0.1.0", src["version"])
	turns := raw["turns"].([]any)
	require.Len(t, turns, 1)
}

func TestNewSession_EmptyTurnsMarshalsAsArray(t *testing.T) {
	s := stem.NewSession("s-1")
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"turns":[]`)
	assert.NotContains(t, string(data), `"turns":null`)
}

func TestFork_EmptyTurnsMarshalsAsArray(t *testing.T) {
	parent := stem.NewSession("p-1")
	child := parent.Fork("c-1")
	data, err := json.Marshal(child)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"turns":[]`)
	assert.NotContains(t, string(data), `"turns":null`)
}

func TestDispatch_EmptyTurnsMarshalsAsArray(t *testing.T) {
	parent := stem.NewSession("p-1")
	part, err := stem.ToolCallPart("call-1", "seeded_tool", map[string]any{})
	require.NoError(t, err)
	parent.Turns = append(parent.Turns, stem.Turn{
		ID:        "seed",
		Role:      stem.RoleAssistant,
		CreatedAt: time.Now().UTC(),
		Content:   []stem.ContentPart{part},
	})
	child := parent.Dispatch(context.Background(), "c-1", "call-1")
	data, err := json.Marshal(child)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"turns":[]`)
	assert.NotContains(t, string(data), `"turns":null`)
}

func TestContentPart_ExtensionRoundTrip(t *testing.T) {
	raw := `{"type":"x-io.jadb.custom","foo":42,"bar":["a","b"]}`
	var p stem.ContentPart
	require.NoError(t, json.Unmarshal([]byte(raw), &p))
	assert.True(t, p.IsExtension())

	out, err := json.Marshal(p)
	require.NoError(t, err)
	assert.JSONEq(t, raw, string(out))
}

func TestImagePart_MarshalRejectsBothDataAndURL(t *testing.T) {
	p := stem.ContentPart{
		Type: stem.PartTypeImage,
		Mime: "image/png",
		Data: "AAAA",
		URL:  "https://example.com/x.png",
	}
	_, err := json.Marshal(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of data/url")
}

func TestImagePart_MarshalRejectsNeitherDataNorURL(t *testing.T) {
	p := stem.ContentPart{
		Type: stem.PartTypeImage,
		Mime: "image/png",
	}
	_, err := json.Marshal(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of data/url")
}

func TestRoleConstants(t *testing.T) {
	roles := []stem.Role{
		stem.RoleUser,
		stem.RoleAssistant,
		stem.RoleTool,
		stem.RoleSystem,
		stem.RoleDeveloper,
	}
	assert.Len(t, roles, 5)
	for _, r := range roles {
		assert.NotEmpty(t, r)
	}
}

func TestSessionMetaJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	meta := stem.SessionMeta{
		ID:        "s-1",
		TurnCount: 10,
		CreatedAt: now,
		UpdatedAt: now,
		ParentID:  "s-0",
		Source:    stem.DefaultSource(),
		Metadata:  map[string]any{"model": "claude"},
	}

	data, err := json.Marshal(meta)
	require.NoError(t, err)

	var got stem.SessionMeta
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, meta.ID, got.ID)
	assert.Equal(t, meta.TurnCount, got.TurnCount)
	assert.Equal(t, meta.ParentID, got.ParentID)
	assert.Equal(t, "claude", got.Metadata["model"])
}
