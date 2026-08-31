package sessions

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"hop.top/stem/adapters"
)

// Filter is the shared recall-axis filter for list and search (spec
// §2.2). Zero values leave the corresponding axis unconstrained.
type Filter struct {
	// Since / Until bound the time window. A session matches when its
	// [created_at, updated_at] interval intersects [Since, Until]
	// (interval intersection, not point-in-window).
	Since, Until time.Time
	// CWD keeps sessions whose recorded working directory equals the
	// path or lies under it (path-segment boundary). Sessions with no
	// recorded cwd never match a non-empty CWD.
	CWD string
	// Sources keeps sessions whose source.kind matches any entry.
	Sources []string
}

// Match reports whether the record passes every configured axis.
func (f Filter) Match(r *Record) bool {
	env := r.Envelope
	if !f.Since.IsZero() && env.UpdatedAt.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && env.CreatedAt.After(f.Until) {
		return false
	}
	if f.CWD != "" {
		cwd := metaString(env, adapters.MetaCWD)
		if cwd == "" || !underPath(cwd, f.CWD) {
			return false
		}
	}
	if len(f.Sources) > 0 && !slices.Contains(f.Sources, env.Source.Kind) {
		return false
	}
	return true
}

// underPath reports whether path equals base or lies under it,
// honoring path-segment boundaries (/a/bc is not under /a/b).
func underPath(path, base string) bool {
	path = filepath.Clean(path)
	base = filepath.Clean(base)
	if path == base {
		return true
	}
	if base == string(filepath.Separator) {
		return strings.HasPrefix(path, base)
	}
	return strings.HasPrefix(path, base+string(filepath.Separator))
}

// Apply returns the records matching f, preserving order.
func Apply(recs []*Record, f Filter) []*Record {
	out := make([]*Record, 0, len(recs))
	for _, r := range recs {
		if f.Match(r) {
			out = append(out, r)
		}
	}
	return out
}

// Sort orders records by updated_at descending (newest first),
// breaking ties by id ascending for deterministic output (spec §2.3).
func Sort(recs []*Record) {
	sort.SliceStable(recs, func(i, j int) bool {
		a, b := recs[i].Envelope, recs[j].Envelope
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return a.ID < b.ID
	})
}

// relativeWhen matches the <N>h / <N>d / <N>w relative time forms.
var relativeWhen = regexp.MustCompile(`^(\d+)([hdw])$`)

// ParseWhen parses a --since/--until value: an RFC 3339 timestamp, a
// YYYY-MM-DD date (local midnight), or a relative duration <N>h /
// <N>d / <N>w counted back from now.
func ParseWhen(s string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	if m := relativeWhen.FindStringSubmatch(s); m != nil {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil {
			unit := map[string]time.Duration{
				"h": time.Hour,
				"d": 24 * time.Hour,
				"w": 7 * 24 * time.Hour,
			}[m[2]]
			return now.Add(-time.Duration(n) * unit), nil
		}
	}
	return time.Time{}, fmt.Errorf(
		"invalid time %q: use RFC 3339, YYYY-MM-DD, or a relative <N>h/<N>d/<N>w", s)
}
