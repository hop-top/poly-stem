// Package commands builds the stem CLI command tree on kit's console
// framework. The sessions group implements the read-only lookup
// surface specified in docs/specs/sessions-cli.md; every leaf
// declares its own explicit `--format text|json` contract (the kit
// global --format suite is disabled for exactly that reason).
package commands

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
)

// CLI wraps the kit root plus the process exit code resolved by the
// leaves. Leaves render their own result and error documents (spec
// §2 output discipline) and record the semantic exit code here
// instead of returning errors, so no framework layer appends a
// second document to either stream.
type CLI struct {
	// Root is the configured kit root; Execute it to run the CLI.
	Root *kitcli.Root

	exit int
}

// New builds the stem command tree. version is stamped by the binary
// via ldflags.
func New(version string) *CLI {
	c := &CLI{}
	c.Root = kitcli.New(kitcli.Config{
		Name:    "stem",
		Version: version,
		Short:   "AI agent runtime and cross-CLI session recall on crtx envelopes",
		// The sessions spec pins a per-leaf `--format text|json`
		// contract (default text); kit's root-level format suite is
		// disabled so each leaf owns its declaration explicitly.
		Disable:           kitcli.Disable{Format: true},
		QuietBootWarnings: true,
	}, kitcli.WithStatus(kitcli.StatusConfig{}))

	c.Root.Cmd.SilenceErrors = true
	c.Root.Cmd.SilenceUsage = true
	// Flag-parse failures are usage errors (exit 2, spec §2.7). The
	// corrective guidance rides in Message — the framework-level
	// renderer prints only Code + Message, so guidance parked in
	// SuggestedFix alone would never reach stderr.
	c.Root.Cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		fix := "run `" + cmd.CommandPath() + " --help` for accepted flags"
		return &output.Error{
			Code:         output.CodeUsage,
			Message:      err.Error() + " — " + fix,
			SuggestedFix: fix,
			ExitCode:     2,
		}
	})
	// A runnable root keeps bare `stem` on the help path (exit 0)
	// while unmatched positionals reject as usage errors — cobra only
	// arg-validates runnable commands, so a bare group would swallow
	// `stem frobnicate` as help-and-exit-0.
	c.Root.Cmd.RunE = runHelp
	c.Root.Cmd.Args = rejectUnknownSubcommand
	kitcli.SetSideEffect(c.Root.Cmd, kitcli.SideEffectRead)
	kitcli.SetIdempotency(c.Root.Cmd, kitcli.IdempotencyYes)

	c.Root.Cmd.AddCommand(c.sessionsCmd())
	c.Root.Cmd.AddCommand(c.capabilitiesCmd())
	return c
}

// ExitCode returns the semantic exit code recorded by the executed
// leaf; 0 when the invocation succeeded.
func (c *CLI) ExitCode() int { return c.exit }

// runHelp is the RunE for group nodes: bare invocation renders help
// and succeeds.
func runHelp(cmd *cobra.Command, _ []string) error { return cmd.Help() }

// rejectUnknownSubcommand turns unmatched positionals on a group
// command into structured usage errors (spec §2.7) instead of
// cobra's silent help-and-exit-0.
func rejectUnknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	fix := "run `" + cmd.CommandPath() + " --help` to see available commands"
	return &output.Error{
		Code:         output.CodeUsage,
		Message:      "unknown command \"" + args[0] + "\" for \"" + cmd.CommandPath() + "\" — " + fix,
		SuggestedFix: fix,
		ExitCode:     2,
	}
}

// ResolveExit maps one Execute outcome to the process exit status.
// Precedence: a structured envelope declares the authoritative
// exit_code; context cancellation exits 130 (128+SIGINT); any other
// framework-level failure is generic (1); a nil error defers to the
// exit code the executed leaf recorded.
func (c *CLI) ResolveExit(err error) int {
	if err == nil {
		return c.exit
	}
	var ce interface{ AsCLIError() *output.Error }
	if errors.As(err, &ce) {
		if e := ce.AsCLIError(); e != nil && e.ExitCode != 0 {
			return e.ExitCode
		}
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	return 1
}
