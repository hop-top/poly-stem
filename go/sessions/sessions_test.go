package sessions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stem "hop.top/stem"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return ts
}

func TestParseWhen(t *testing.T) {
	now := mustParse(t, "2026-08-08T12:00:00Z")

	t.Run("rfc3339", func(t *testing.T) {
		got, err := ParseWhen("2026-08-07T09:14:56Z", now)
		require.NoError(t, err)
		assert.Equal(t, mustParse(t, "2026-08-07T09:14:56Z"), got)
	})

	t.Run("date", func(t *testing.T) {
		got, err := ParseWhen("2026-08-07", now)
		require.NoError(t, err)
		assert.Equal(t, 2026, got.Year())
		assert.Equal(t, time.August, got.Month())
		assert.Equal(t, 7, got.Day())
	})

	t.Run("relative", func(t *testing.T) {
		for in, want := range map[string]time.Time{
			"12h": now.Add(-12 * time.Hour),
			"3d":  now.Add(-3 * 24 * time.Hour),
			"2w":  now.Add(-2 * 7 * 24 * time.Hour),
		} {
			got, err := ParseWhen(in, now)
			require.NoError(t, err, in)
			assert.Equal(t, want, got, in)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		for _, in := range []string{"", "yesterday", "3x", "h", "-3d", "2026-13-40"} {
			_, err := ParseWhen(in, now)
			assert.Error(t, err, "%q must be rejected", in)
		}
	})
}

func rec(id, cwd, created, updated string) *Record {
	env := &stem.Session{
		CrtxVersion: "0.1",
		ID:          id,
		Source:      stem.Source{Kind: "stem", Version: "0.1.0"},
	}
	env.CreatedAt, _ = time.Parse(time.RFC3339, created)
	env.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	if cwd != "" {
		env.Metadata = map[string]any{"top.hop.stem.cwd": cwd}
	}
	return &Record{Envelope: env, Store: StoreRef{Adapter: "crtx", Path: "/x/" + id + ".jsonl"}}
}

func TestFilterIntervalIntersection(t *testing.T) {
	// Session runs 10:00 -> 12:00.
	r := rec("0198f2d4-0000-7000-8000-000000000001", "",
		"2026-08-07T10:00:00Z", "2026-08-07T12:00:00Z")

	cases := []struct {
		name         string
		since, until string
		want         bool
	}{
		{"window inside span", "2026-08-07T10:30:00Z", "2026-08-07T11:00:00Z", true},
		{"span inside window", "2026-08-07T00:00:00Z", "2026-08-08T00:00:00Z", true},
		{"overlap start", "2026-08-07T11:00:00Z", "2026-08-07T23:00:00Z", true},
		{"overlap end", "2026-08-07T01:00:00Z", "2026-08-07T10:30:00Z", true},
		{"before window", "2026-08-07T13:00:00Z", "2026-08-07T14:00:00Z", false},
		{"after window", "2026-08-07T01:00:00Z", "2026-08-07T09:00:00Z", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := Filter{Since: mustParse(t, tc.since), Until: mustParse(t, tc.until)}
			assert.Equal(t, tc.want, f.Match(r))
		})
	}
}

func TestFilterCwdSubtree(t *testing.T) {
	base := "/home/dev/acme"
	cases := []struct {
		cwd  string
		want bool
	}{
		{"/home/dev/acme", true},
		{"/home/dev/acme/sub", true},
		{"/home/dev/acme/sub/deep", true},
		{"/home/dev/acmebis", false}, // segment boundary, not string prefix
		{"/home/dev", false},
		{"", false}, // no recorded cwd never matches
	}
	for _, tc := range cases {
		r := rec("0198f2d4-0000-7000-8000-000000000002", tc.cwd,
			"2026-08-07T10:00:00Z", "2026-08-07T10:00:00Z")
		f := Filter{CWD: base}
		assert.Equal(t, tc.want, f.Match(r), "cwd=%q", tc.cwd)
	}
}

func TestFilterSource(t *testing.T) {
	r := rec("0198f2d4-0000-7000-8000-000000000003", "",
		"2026-08-07T10:00:00Z", "2026-08-07T10:00:00Z")
	assert.True(t, Filter{Sources: []string{"codex", "stem"}}.Match(r))
	assert.False(t, Filter{Sources: []string{"codex"}}.Match(r))
	assert.True(t, Filter{}.Match(r), "no source filter matches everything")
}

func TestSortNewestFirstIDTieBreak(t *testing.T) {
	a := rec("bbbbbbbb-0000-7000-8000-000000000001", "", "2026-08-07T10:00:00Z", "2026-08-07T10:00:00Z")
	b := rec("aaaaaaaa-0000-7000-8000-000000000001", "", "2026-08-07T10:00:00Z", "2026-08-07T10:00:00Z")
	c := rec("cccccccc-0000-7000-8000-000000000001", "", "2026-08-07T09:00:00Z", "2026-08-07T11:00:00Z")
	recs := []*Record{a, b, c}
	Sort(recs)
	assert.Equal(t, []*Record{c, b, a}, recs,
		"updated_at descending, then id ascending")
}

func TestResolve(t *testing.T) {
	recs := []*Record{
		rec("0198aa00-1111-7abc-8def-123456789abc", "", "2026-08-05T17:00:00Z", "2026-08-05T18:02:00Z"),
		rec("0198bb00-2222-7abc-8def-aabbccddeeff", "", "2026-08-07T08:30:00Z", "2026-08-07T09:14:56Z"),
		rec("9a1f0e2c-3b4d-4a5e-8f60-712233445566", "", "2026-08-06T08:00:00Z", "2026-08-06T09:00:00Z"),
	}

	t.Run("unique prefix", func(t *testing.T) {
		got, _, err := Resolve(recs, "9a1f")
		require.NoError(t, err)
		assert.Equal(t, "9a1f0e2c-3b4d-4a5e-8f60-712233445566", got.Envelope.ID)
	})

	t.Run("case insensitive", func(t *testing.T) {
		got, _, err := Resolve(recs, "9A1F0E2C")
		require.NoError(t, err)
		assert.Equal(t, "9a1f0e2c-3b4d-4a5e-8f60-712233445566", got.Envelope.ID)
	})

	t.Run("ambiguous", func(t *testing.T) {
		got, candidates, err := Resolve(recs, "0198")
		assert.ErrorIs(t, err, ErrAmbiguous)
		assert.Nil(t, got)
		require.Len(t, candidates, 2)
		assert.Equal(t, "0198bb00-2222-7abc-8def-aabbccddeeff", candidates[0].Envelope.ID,
			"candidates ordered newest first")
	})

	t.Run("not found", func(t *testing.T) {
		_, _, err := Resolve(recs, "ffff0000")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("too short", func(t *testing.T) {
		_, _, err := Resolve(recs, "919")
		assert.ErrorIs(t, err, ErrRefTooShort)
	})

	t.Run("exact full id beats prefix ambiguity", func(t *testing.T) {
		withNested := append([]*Record{}, recs...)
		withNested = append(withNested,
			rec("0198aa00-1111-7abc-8def-123456789abc-x", "", "2026-08-01T00:00:00Z", "2026-08-01T00:00:00Z"))
		got, _, err := Resolve(withNested, "0198aa00-1111-7abc-8def-123456789abc")
		require.NoError(t, err)
		assert.Equal(t, "0198aa00-1111-7abc-8def-123456789abc", got.Envelope.ID)
	})
}
