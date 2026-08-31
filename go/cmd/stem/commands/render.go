package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	stem "hop.top/stem"
	"hop.top/stem/sessions"
)

// Text rendering for the sessions leaves. Text output is not
// contractual (spec §3) and favors human scanning: 8-char id
// prefixes, ~-shortened paths, truncated titles.

const (
	idPrefixLen   = 8
	maxTitleCol   = 60
	maxPreviewLen = 80
	timestampCol  = "2006-01-02 15:04"
)

func idPrefix(id string) string {
	if len(id) <= idPrefixLen {
		return id
	}
	return id[:idPrefixLen]
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}

func orDash(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
}

func truncateCol(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

// renderListText writes the aligned session table. Empty input
// writes nothing: zero results is success with an empty result set.
func renderListText(w io.Writer, sums []sessions.Summary) {
	if len(sums) == 0 {
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSOURCE\tUPDATED\tTURNS\tCWD\tTITLE")
	for _, s := range sums {
		cwd := "-"
		if s.CWD != nil {
			cwd = shortenHome(*s.CWD)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
			idPrefix(s.ID),
			s.Source.Kind,
			s.UpdatedAt.Local().Format(timestampCol),
			s.TurnCount,
			cwd,
			truncateCol(orDash(s.Title), maxTitleCol),
		)
	}
	_ = tw.Flush()
}

// emitEnvelope writes the session's crtx envelope as one JSON
// document: the stored line verbatim for crtx-native sessions, the
// normalized envelope otherwise (spec §2.6).
func emitEnvelope(w io.Writer, rec *sessions.Record) error {
	if rec.Raw != nil {
		if _, err := w.Write(rec.Raw); err != nil {
			return err
		}
		_, err := io.WriteString(w, "\n")
		return err
	}
	data, err := json.Marshal(rec.Envelope)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err = io.WriteString(w, "\n")
	return err
}

// renderShowText writes the header + turn-by-turn view (spec §2.6).
func renderShowText(w io.Writer, rec *sessions.Record) {
	env := rec.Envelope
	sum := sessions.Summarize(rec)

	fmt.Fprintf(w, "id:       %s\n", env.ID)
	fmt.Fprintf(w, "source:   %s %s\n", env.Source.Kind, env.Source.Version)
	fmt.Fprintf(w, "created:  %s\n", env.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "updated:  %s\n", env.UpdatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "cwd:      %s\n", orDash(sum.CWD))
	fmt.Fprintf(w, "branch:   %s\n", orDash(sum.GitBranch))
	if env.ParentID != "" {
		fork := ""
		if env.ForkPoint != nil {
			fork = fmt.Sprintf(" (fork @%d)", *env.ForkPoint)
		}
		fmt.Fprintf(w, "parent:   %s%s\n", env.ParentID, fork)
	}
	if env.DispatchedFrom != nil {
		fmt.Fprintf(w, "dispatched from: %s (call %s)\n",
			env.DispatchedFrom.EnvelopeID, env.DispatchedFrom.CallID)
	}
	fmt.Fprintf(w, "store:    %s %s\n", rec.Store.Adapter, rec.Store.Path)

	for i, turn := range env.Turns {
		fmt.Fprintf(w, "\n[%d] %s  %s\n", i, turn.Role, turn.CreatedAt.Format(time.RFC3339))
		for _, part := range turn.Content {
			renderPartText(w, part)
		}
	}
}

func renderPartText(w io.Writer, part stem.ContentPart) {
	switch part.Type {
	case stem.PartTypeText:
		for _, line := range strings.Split(part.Text, "\n") {
			fmt.Fprintf(w, "    %s\n", line)
		}
	case stem.PartTypeThinking:
		fmt.Fprintln(w, "    [thinking]")
	case stem.PartTypeToolCall:
		fmt.Fprintf(w, "    [tool_call %s %s] %s\n",
			part.Name, part.CallID, preview(part.Input))
	case stem.PartTypeToolResult:
		suffix := ""
		if part.IsError {
			suffix = " (error)"
		}
		fmt.Fprintf(w, "    [tool_result %s]%s %s\n",
			part.CallID, suffix, preview(part.Output))
	case stem.PartTypeImage:
		fmt.Fprintf(w, "    [image %s]\n", part.Mime)
	default:
		fmt.Fprintf(w, "    [%s]\n", part.Type)
	}
}

// preview renders a truncated single-line view of a raw JSON payload.
func preview(raw json.RawMessage) string {
	s := sessions.CollapseSpace(string(raw))
	return truncateCol(s, maxPreviewLen)
}
