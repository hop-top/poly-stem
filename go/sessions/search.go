package sessions

import (
	"fmt"
	"regexp"
	"sort"
	"unicode/utf8"

	stem "hop.top/stem"
)

// Search scopes (spec §2.4).
const (
	ScopeContent  = "content"
	ScopeMetadata = "metadata"
	ScopeAll      = "all"
)

// Snippet sizing: bytes of context kept on each side of the matched
// region before whitespace collapsing, and the rune cap of the final
// snippet.
const (
	snippetContext = 80
	snippetMaxLen  = 200
)

// CompileQuery builds the RE2 matcher behind both matching modes:
// fixed-string containment (metacharacters quoted) or a raw RE2
// pattern, case-folded unless caseSensitive.
func CompileQuery(query string, regex, caseSensitive bool) (*regexp.Regexp, error) {
	expr := query
	if !regex {
		expr = regexp.QuoteMeta(query)
	}
	if !caseSensitive {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression %q: %v", query, err)
	}
	return re, nil
}

// Query is one compiled search request.
type Query struct {
	// Pattern is the compiled matcher (CompileQuery).
	Pattern *regexp.Regexp
	// Scope is ScopeContent, ScopeMetadata, or ScopeAll.
	Scope string
	// Role restricts content matches to turns of this role; empty
	// matches every role. Metadata hits are unaffected.
	Role stem.Role
	// Tools extends the content scope to tool_call input and
	// tool_result output.
	Tools bool
}

// Hit is one match within a session (spec §3.3). Content hits carry
// turn coordinates; metadata hits carry nulls.
type Hit struct {
	Scope     string  `json:"scope"`
	TurnIndex *int    `json:"turn_index"`
	TurnID    *string `json:"turn_id"`
	Role      *string `json:"role"`
	Snippet   string  `json:"snippet"`
}

// Match groups a session's hits under its summary (spec §3.3).
type Match struct {
	Session Summary `json:"session"`
	Hits    []Hit   `json:"hits"`
}

// Search runs the query over every record, returning one Match per
// session with at least one hit. Record order is preserved (callers
// pass the newest-first scan output); hits within a session follow
// turn order, with metadata hits appended after content hits.
func Search(recs []*Record, q Query) []Match {
	var out []Match
	for _, rec := range recs {
		hits := searchRecord(rec, q)
		if len(hits) == 0 {
			continue
		}
		out = append(out, Match{Session: Summarize(rec), Hits: hits})
	}
	return out
}

func searchRecord(rec *Record, q Query) []Hit {
	var hits []Hit
	if q.Scope == ScopeContent || q.Scope == ScopeAll {
		hits = append(hits, contentHits(rec.Envelope, q)...)
	}
	if q.Scope == ScopeMetadata || q.Scope == ScopeAll {
		hits = append(hits, metadataHits(rec.Envelope, q)...)
	}
	return hits
}

// contentHits scans text and thinking parts of every turn (plus tool
// payloads with Tools), emitting at most one hit per turn: the first
// matching part decides the snippet.
func contentHits(env *stem.Session, q Query) []Hit {
	var hits []Hit
	for i, turn := range env.Turns {
		if q.Role != "" && turn.Role != q.Role {
			continue
		}
		for _, part := range turn.Content {
			text, ok := searchableText(part, q.Tools)
			if !ok {
				continue
			}
			loc := q.Pattern.FindStringIndex(text)
			if loc == nil {
				continue
			}
			idx := i
			turnID := turn.ID
			role := string(turn.Role)
			hits = append(hits, Hit{
				Scope:     ScopeContent,
				TurnIndex: &idx,
				TurnID:    &turnID,
				Role:      &role,
				Snippet:   Snippet(text, loc),
			})
			break
		}
	}
	return hits
}

// searchableText returns the query-visible text of a content part:
// text and thinking always; tool_call input and tool_result output
// only when tools is set (spec §2.4).
func searchableText(part stem.ContentPart, tools bool) (string, bool) {
	switch part.Type {
	case stem.PartTypeText, stem.PartTypeThinking:
		return part.Text, true
	case stem.PartTypeToolCall:
		if tools {
			return string(part.Input), true
		}
	case stem.PartTypeToolResult:
		if tools {
			return string(part.Output), true
		}
	}
	return "", false
}

// metadataHits scans the derived title and every scalar metadata
// value, keys in sorted order for determinism.
func metadataHits(env *stem.Session, q Query) []Hit {
	var hits []Hit
	emit := func(text string) {
		if loc := q.Pattern.FindStringIndex(text); loc != nil {
			hits = append(hits, Hit{Scope: ScopeMetadata, Snippet: Snippet(text, loc)})
		}
	}
	if title := DeriveTitle(env); title != "" {
		emit(title)
	}
	keys := make([]string, 0, len(env.Metadata))
	for k := range env.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		// Scalars only: strings match directly; JSON numbers and bools
		// (float64/bool after unmarshal) match their printed form.
		// Composite values stay out of scope.
		switch v := env.Metadata[k].(type) {
		case string:
			emit(v)
		case bool, float64, int, int64:
			emit(fmt.Sprint(v))
		}
	}
	return hits
}

// Snippet renders the matched region with surrounding context:
// snippetContext bytes each side (snapped to rune boundaries),
// whitespace collapsed, capped at snippetMaxLen runes, ellipses
// marking clipped ends (spec §2.4).
func Snippet(text string, loc []int) string {
	start := loc[0] - snippetContext
	if start < 0 {
		start = 0
	}
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	end := loc[1] + snippetContext
	if end > len(text) {
		end = len(text)
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}

	s := CollapseSpace(text[start:end])
	if start > 0 {
		s = "…" + s
	}
	clippedRight := end < len(text)
	if utf8.RuneCountInString(s) > snippetMaxLen {
		runes := []rune(s)
		s = string(runes[:snippetMaxLen-1])
		clippedRight = true
	}
	if clippedRight {
		s += "…"
	}
	return s
}
