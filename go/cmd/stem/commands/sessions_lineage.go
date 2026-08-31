package commands

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"hop.top/stem/sessions"
)

// lineageDoc is the lineage result document (spec §3.5), pinned by
// contracts/sessions/lineage.yaml: the queried session's full id and
// a flat node array, root first, then breadth-first.
type lineageDoc struct {
	Target string                 `json:"target"`
	Nodes  []sessions.LineageNode `json:"nodes"`
}

func (c *CLI) sessionsLineageCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "lineage <id>",
		Short: "Fork/dispatch chain around a session, across CLIs",
		Long: `Resolve the continuation chain around one session, addressed by full
id or case-insensitive prefix (minimum 4 characters): ancestors are
walked to the root via fork (parent_id/fork_point) and dispatch
(dispatched_from) references, descendants are the transitive children
of the queried session. Resolution runs over every scanned store, so
a chain may cross CLIs.

A parent referenced but found in no store renders as an unresolved
placeholder node. Text output draws the chain as a tree, root first,
the queried session marked; --format json emits the flat node array
pinned by the lineage contract. Exit 3 when nothing matches, exit 4
with up to 10 structured candidates when the prefix is ambiguous.`,
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
					"usage: stem sessions lineage <id>", nil)
			}

			recs := scan(s)
			rec, ok := c.resolveRef(s, recs, args[0])
			if !ok {
				return nil
			}

			nodes := sessions.Lineage(recs, rec)
			if s.format == formatJSON {
				if err := s.emitJSON(lineageDoc{Target: rec.Envelope.ID, Nodes: nodes}); err != nil {
					return c.fail(s, exitInternal, err.Error(), "", nil)
				}
			} else {
				renderLineageText(cmd.OutOrStdout(), nodes, rec.Envelope.ID)
			}
			s.flushWarnings()
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", formatText, "Output format: text or json")
	markReadLeaf(cmd)
	return cmd
}

// renderLineageText draws the chain as a tree, root first, the
// queried session marked (spec §2.5). Text output is not contractual.
func renderLineageText(w io.Writer, nodes []sessions.LineageNode, target string) {
	if len(nodes) == 0 {
		return
	}
	kids := make(map[string][]int)
	for i := 1; i < len(nodes); i++ {
		if p := nodes[i].Parent; p != nil {
			kids[*p] = append(kids[*p], i)
		}
	}
	var render func(idx int, prefix string)
	render = func(idx int, prefix string) {
		c := kids[nodes[idx].ID]
		for i, ci := range c {
			connector, cont := "├─ ", "│  "
			if i == len(c)-1 {
				connector, cont = "└─ ", "   "
			}
			fmt.Fprintf(w, "%s%s%s\n", prefix, connector, lineageEdgeLabel(nodes[ci]))
			fmt.Fprintf(w, "%s%s%s\n", prefix, cont, lineageNodeLine(nodes[ci], target))
			render(ci, prefix+cont)
		}
	}
	fmt.Fprintln(w, lineageNodeLine(nodes[0], target))
	render(0, "")
}

// lineageEdgeLabel names the edge a node rides to its parent.
func lineageEdgeLabel(n sessions.LineageNode) string {
	if n.Relation == nil {
		return ""
	}
	if *n.Relation == sessions.RelationDispatch {
		call := "?"
		if n.CallID != nil {
			call = *n.CallID
		}
		return fmt.Sprintf("dispatch (call %s)", call)
	}
	if n.ForkPoint != nil {
		return fmt.Sprintf("fork @t%d", *n.ForkPoint)
	}
	return "fork"
}

// lineageNodeLine renders one node: id prefix, source, updated date,
// title; the queried session is marked, unresolved placeholders say
// so.
func lineageNodeLine(n sessions.LineageNode, target string) string {
	if !n.Resolved || n.Summary == nil {
		return fmt.Sprintf("%s  (unresolved)", idPrefix(n.ID))
	}
	s := n.Summary
	line := fmt.Sprintf("%s  %s  %s  %s",
		idPrefix(n.ID),
		s.Source.Kind,
		s.UpdatedAt.Local().Format("2006-01-02"),
		truncateCol(orDash(s.Title), maxTitleCol))
	if n.ID == target {
		line += "  ◀ target"
	}
	return line
}
