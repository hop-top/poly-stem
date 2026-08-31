package commands

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"hop.top/kit/go/ai/toolspec"
	speccli "hop.top/kit/go/ai/toolspec/cli"
)

// capabilitiesSchemaVersion is the CLI-surface schema version the
// capabilities document claims. Distinct from the binary semver: it
// evolves when the introspection shape changes.
const capabilitiesSchemaVersion = "1.0"

// capabilityLeaf is one invocable leaf in the capabilities document:
// full command path, leaf name, contract annotations, and the leaf's
// local flag surface.
type capabilityLeaf struct {
	Path       string                  `json:"path"`
	Name       string                  `json:"name"`
	Short      string                  `json:"short,omitempty"`
	SideEffect string                  `json:"side_effect,omitempty"`
	Idempotent string                  `json:"idempotent,omitempty"`
	DryRun     bool                    `json:"dry_run_supported"`
	Flags      []toolspec.ManifestFlag `json:"flags,omitempty"`
}

// capabilitiesDoc is the machine-readable capability surface the
// binary serves: agents parse leaves[].path / leaves[].name to
// discover what they may invoke without scraping help text.
type capabilitiesDoc struct {
	Tool          string           `json:"tool"`
	Version       string           `json:"version"`
	SchemaVersion string           `json:"schema_version"`
	Leaves        []capabilityLeaf `json:"leaves"`
}

// capabilitiesCmd mounts the capability introspection leaf. The
// projection reuses kit's toolspec walker (the machinery behind
// `<tool> spec`) so annotations, deprecation filtering, and flag
// discovery stay kit-canonical; only the emitted shape differs — a
// flat leaves list keyed by string path, rendered through the
// sessions-spec format contract (per-leaf --format text|json).
func (c *CLI) capabilitiesCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "capabilities",
		Short: "Emit the machine-readable command surface",
		Long: `Describe every invocable leaf of the stem CLI as structured data:
full command path, leaf name, side-effect and idempotency contract,
dry-run support, and per-leaf flags. Agents consume this instead of
scraping help text; humans get an aligned table.

Read-only. --format json emits the full document; text renders the
leaf table. The document mirrors the live command tree, so a leaf
listed here is invocable as printed.`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			"kit/top-level-verb": "true",
			// Skip the deprecation-warning middleware so the emitted
			// document stream stays clean (same marker kit's own spec
			// command carries). The manifest walker still lists this
			// leaf — it is part of the invocable surface.
			"kit/spec-command": "true",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, ok := c.newSink(cmd, format)
			if !ok {
				return nil
			}
			m := speccli.EmitManifest(c.Root, capabilitiesSchemaVersion)
			doc := capabilitiesDoc{
				Tool:          m.Tool,
				Version:       m.Version,
				SchemaVersion: m.SchemaVersion,
				Leaves:        make([]capabilityLeaf, 0, len(m.Commands)),
			}
			for _, mc := range m.Commands {
				if len(mc.Path) == 0 {
					continue
				}
				doc.Leaves = append(doc.Leaves, capabilityLeaf{
					Path:       strings.Join(mc.Path, " "),
					Name:       mc.Path[len(mc.Path)-1],
					Short:      mc.Short,
					SideEffect: mc.SideEffect,
					Idempotent: mc.Idempotent,
					DryRun:     mc.DryRunSupported,
					Flags:      mc.Flags,
				})
			}
			if s.format == formatJSON {
				if err := s.emitJSON(doc); err != nil {
					return c.fail(s, exitInternal, err.Error(), "", nil)
				}
			} else {
				renderCapabilitiesText(cmd.OutOrStdout(), doc.Leaves)
			}
			s.flushWarnings()
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", formatText, "Output format: text or json")
	markReadLeaf(cmd)
	return cmd
}

// renderCapabilitiesText writes the aligned leaf table.
func renderCapabilitiesText(w io.Writer, leaves []capabilityLeaf) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PATH\tSIDE-EFFECT\tIDEMPOTENT\tDESCRIPTION")
	for _, l := range leaves {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", l.Path, l.SideEffect, l.Idempotent, l.Short)
	}
	_ = tw.Flush()
}
