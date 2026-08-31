package commands

import (
	"github.com/spf13/cobra"
)

// maxCandidates caps the structured candidate list on ambiguous-id
// errors (spec §3.6).
const maxCandidates = 10

func (c *CLI) sessionsShowCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one session turn by turn",
		Long: `Print one session addressed by full id or case-insensitive prefix
(minimum 4 characters). Text output renders a header followed by
every turn; thinking parts collapse to a marker and tool payloads
render as truncated previews.

--format json emits the normalized crtx v0.1 envelope verbatim (no
wrapper) — the universal escape hatch for crtx-aware consumers.
Exit 3 when nothing matches, exit 4 with up to 10 structured
candidates when the prefix is ambiguous.`,
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
					"exactly one session id argument is required",
					"usage: stem sessions show <id>", nil)
			}
			recs := scan(s)
			rec, ok := c.resolveRef(s, recs, args[0])
			if !ok {
				return nil
			}

			if s.format == formatJSON {
				if err := emitEnvelope(cmd.OutOrStdout(), rec); err != nil {
					return c.fail(s, exitInternal, err.Error(), "", nil)
				}
			} else {
				renderShowText(cmd.OutOrStdout(), rec)
			}
			s.flushWarnings()
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", formatText, "Output format: text or json")
	markReadLeaf(cmd)
	return cmd
}
