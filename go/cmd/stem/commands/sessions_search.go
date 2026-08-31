package commands

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	stem "hop.top/stem"

	"hop.top/stem/sessions"
)

// searchDoc is the `search --format json` document (spec §3.3),
// pinned by contracts/sessions/search.yaml.
type searchDoc struct {
	Query     string           `json:"query"`
	Matches   []sessions.Match `json:"matches"`
	Truncated bool             `json:"truncated"`
}

func (c *CLI) sessionsSearchCmd() *cobra.Command {
	var (
		ff            filterFlags
		format        string
		regex         bool
		caseSensitive bool
		scope         string
		role          string
		tools         bool
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Find sessions by content or metadata",
		Long: `Search every known session for a query: case-insensitive
fixed-string containment by default, RE2 with --regex, literal case
with --case-sensitive. The content scope covers text and thinking
parts of every turn (tool payloads only with --tools); the metadata
scope covers envelope metadata values and the derived title;
--scope all (the default) covers both. Dispatch-nested sub-agent
transcripts are always searched.

Results group per session, newest first, hits in turn order, each
with a collapsed snippet of the matched region. --format json emits
the document pinned by contracts/sessions/search.yaml.`,
		// Argument-count validation happens in RunE so violations
		// render as structured usage errors (spec §3.6).
		Args: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			s, ok := c.newSink(cmd, format)
			if !ok {
				return nil
			}
			if len(args) != 1 {
				return c.fail(s, exitUsage,
					"exactly one query argument is required",
					"usage: stem sessions search <query>; use `stem sessions list` for filter-only enumeration", nil)
			}
			if scope != sessions.ScopeContent && scope != sessions.ScopeMetadata && scope != sessions.ScopeAll {
				return c.fail(s, exitUsage,
					fmt.Sprintf("invalid --scope %q: use content, metadata, or all", scope), "", nil)
			}
			pattern, err := sessions.CompileQuery(args[0], regex, caseSensitive)
			if err != nil {
				return c.fail(s, exitUsage, err.Error(),
					"RE2 syntax applies under --regex; drop --regex for literal matching", nil)
			}
			filter, err := ff.build(time.Now())
			if err != nil {
				return c.fail(s, exitUsage, err.Error(),
					"see `stem sessions search --help` for accepted values", nil)
			}

			// Unlike list, search includes dispatch nests: recall must
			// see sub-agent transcripts (spec §2.4).
			recs := sessions.Apply(scan(s), filter)
			matches := sessions.Search(recs, sessions.Query{
				Pattern: pattern,
				Scope:   scope,
				Role:    stem.Role(role),
				Tools:   tools,
			})
			total := len(matches)
			truncated := false
			if ff.limit > 0 && total > ff.limit {
				matches = matches[:ff.limit]
				truncated = true
				s.warnf("search truncated to %d of %d matching sessions; raise --limit", len(matches), total)
			}
			if matches == nil {
				matches = []sessions.Match{}
			}

			if s.format == formatJSON {
				if err := s.emitJSON(searchDoc{Query: args[0], Matches: matches, Truncated: truncated}); err != nil {
					return c.fail(s, exitInternal, err.Error(), "", nil)
				}
			} else {
				renderSearchText(cmd.OutOrStdout(), matches)
			}
			s.flushWarnings()
			return nil
		},
	}
	addFilterFlags(cmd, &ff)
	cmd.Flags().StringVar(&format, "format", formatText, "Output format: text or json")
	cmd.Flags().BoolVar(&regex, "regex", false, "Treat the query as an RE2 regular expression")
	cmd.Flags().BoolVar(&caseSensitive, "case-sensitive", false, "Match with literal case (either mode)")
	cmd.Flags().StringVar(&scope, "scope", sessions.ScopeAll, "Search scope: content, metadata, or all")
	cmd.Flags().StringVar(&role, "role", "", "Restrict content matches to turns of this role")
	cmd.Flags().BoolVar(&tools, "tools", false, "Include tool_call input and tool_result output in the content scope")
	markReadLeaf(cmd)
	return cmd
}

// renderSearchText writes the grep-like grouped view (spec §2.4).
func renderSearchText(w io.Writer, matches []sessions.Match) {
	for i, m := range matches {
		if i > 0 {
			fmt.Fprintln(w)
		}
		s := m.Session
		cwd := "-"
		if s.CWD != nil {
			cwd = shortenHome(*s.CWD)
		}
		fmt.Fprintf(w, "%s  %s  %s  %s\n",
			idPrefix(s.ID), s.Source.Kind, s.UpdatedAt.Local().Format("2006-01-02"), cwd)
		for _, h := range m.Hits {
			if h.Scope == sessions.ScopeMetadata {
				fmt.Fprintf(w, "  [meta]  %s\n", h.Snippet)
				continue
			}
			fmt.Fprintf(w, "  [t%d %s]  %s\n", *h.TurnIndex, *h.Role, h.Snippet)
		}
	}
}
