package sessions

import (
	"strings"
	"time"

	stem "hop.top/stem"
	"hop.top/stem/adapters"
)

// maxTitleLength caps the derived title (runes).
const maxTitleLength = 200

// Summary is the SessionSummary shape shared by list, search, and
// ambiguous-id candidates (spec §3.1). Every field is always
// emitted; nullable fields are pointers so unknown renders as null,
// never as absent.
type Summary struct {
	ID             string               `json:"id"`
	Source         stem.Source          `json:"source"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
	TurnCount      int                  `json:"turn_count"`
	CWD            *string              `json:"cwd"`
	GitBranch      *string              `json:"git_branch"`
	Title          *string              `json:"title"`
	ParentID       *string              `json:"parent_id"`
	ForkPoint      *int                 `json:"fork_point"`
	DispatchedFrom *stem.DispatchedFrom `json:"dispatched_from"`
	Store          StoreRef             `json:"store"`
}

// Summarize denormalizes one record into the shared summary shape.
func Summarize(r *Record) Summary {
	env := r.Envelope
	s := Summary{
		ID:             env.ID,
		Source:         env.Source,
		CreatedAt:      env.CreatedAt,
		UpdatedAt:      env.UpdatedAt,
		TurnCount:      len(env.Turns),
		ForkPoint:      env.ForkPoint,
		DispatchedFrom: env.DispatchedFrom,
		Store:          r.Store,
	}
	if cwd := metaString(env, adapters.MetaCWD); cwd != "" {
		s.CWD = &cwd
	}
	if branch := metaString(env, adapters.MetaGitBranch); branch != "" {
		s.GitBranch = &branch
	}
	if title := DeriveTitle(env); title != "" {
		s.Title = &title
	}
	if env.ParentID != "" {
		pid := env.ParentID
		s.ParentID = &pid
	}
	return s
}

// DeriveTitle returns the session's derived title: the first user
// text part, whitespace-collapsed and length-capped (spec §2.3).
// Empty when the session has no user text.
func DeriveTitle(env *stem.Session) string {
	for _, turn := range env.Turns {
		if turn.Role != stem.RoleUser {
			continue
		}
		for _, part := range turn.Content {
			if part.Type != stem.PartTypeText {
				continue
			}
			if title := CollapseSpace(part.Text); title != "" {
				return truncateRunes(title, maxTitleLength)
			}
		}
	}
	return ""
}

// CollapseSpace collapses every whitespace run to one space and
// trims the ends.
func CollapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
