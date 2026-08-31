package commands

import (
	"time"

	"github.com/spf13/cobra"

	"hop.top/stem/sessions"
)

// listDoc is the `list --format json` document (spec §3.2), pinned
// by contracts/sessions/list.yaml.
type listDoc struct {
	Sessions  []sessions.Summary `json:"sessions"`
	Truncated bool               `json:"truncated"`
}

func (c *CLI) sessionsListCmd() *cobra.Command {
	var (
		ff     filterFlags
		format string
		nested bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sessions across every store, newest first",
		Long: `Enumerate sessions across the crtx session roots and every native
CLI store (Claude Code, Codex, Copilot), deduplicated and ordered by update
time descending. Dispatch-nested sub-agent transcripts are excluded
unless --nested is given; fork children are always included.

Zero results is success. Text output shows 8-character id prefixes;
--format json emits the full document pinned by
contracts/sessions/list.yaml.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, ok := c.newSink(cmd, format)
			if !ok {
				return nil
			}
			filter, err := ff.build(time.Now())
			if err != nil {
				return c.fail(s, exitUsage, err.Error(),
					"see `stem sessions list --help` for accepted values", nil)
			}

			recs := sessions.Apply(scan(s), filter)
			if !nested {
				kept := recs[:0]
				for _, r := range recs {
					if r.Envelope.DispatchedFrom == nil {
						kept = append(kept, r)
					}
				}
				recs = kept
			}
			total := len(recs)
			recs, truncated := truncateRecords(recs, ff.limit)
			if truncated {
				s.warnf("list truncated to %d of %d sessions; raise --limit", len(recs), total)
			}

			if s.format == formatJSON {
				if err := s.emitJSON(listDoc{Sessions: summarize(recs), Truncated: truncated}); err != nil {
					return c.fail(s, exitInternal, err.Error(), "", nil)
				}
			} else {
				renderListText(cmd.OutOrStdout(), summarize(recs))
			}
			s.flushWarnings()
			return nil
		},
	}
	addFilterFlags(cmd, &ff)
	cmd.Flags().StringVar(&format, "format", formatText, "Output format: text or json")
	cmd.Flags().BoolVar(&nested, "nested", false, "Include dispatch-nested sub-agent sessions")
	markReadLeaf(cmd)
	return cmd
}
