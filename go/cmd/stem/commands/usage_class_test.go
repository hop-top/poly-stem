package commands_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"hop.top/kit/go/console/output"

	"hop.top/stem/cmd/stem/commands"
)

// runCLIWithErr is runCLI plus the raw Execute error, for asserting
// the structured usage envelope cobra parse failures must carry.
func runCLIWithErr(t *testing.T, args ...string) (stdout, stderr string, exit int, err error) {
	t.Helper()
	c := commands.New("test")
	var out, errb bytes.Buffer
	c.Root.Cmd.SetOut(&out)
	c.Root.Cmd.SetErr(&errb)
	c.Root.Cmd.SetArgs(args)
	err = c.Root.Cmd.ExecuteContext(context.Background())
	return out.String(), errb.String(), c.ResolveExit(err), err
}

// usageEnvelope unwraps err into the *output.Error the process must
// render, requiring the usage class.
func usageEnvelope(t *testing.T, err error) *output.Error {
	t.Helper()
	require.Error(t, err)
	var ce interface{ AsCLIError() *output.Error }
	require.True(t, errors.As(err, &ce), "error %v is not a structured envelope", err)
	env := ce.AsCLIError()
	require.NotNil(t, env)
	require.Equal(t, output.CodeUsage, env.Code)
	require.Equal(t, 2, env.ExitCode)
	return env
}

// Spec §2.7: an unknown command is a usage error (exit 2), not
// help-and-exit-0.
func TestUnknownTopLevelCommandIsUsageError(t *testing.T) {
	stdout, _, exit, err := runCLIWithErr(t, "frobnicate")
	require.Equal(t, 2, exit)
	env := usageEnvelope(t, err)
	require.Contains(t, env.Message, "unknown command")
	require.Contains(t, env.Message, "frobnicate")
	// Guidance must ride in Message: the framework renderer prints
	// only Code + Message, so this is what reaches stderr.
	require.Contains(t, env.Message, "--help")
	require.Contains(t, env.SuggestedFix, "--help")
	require.NotContains(t, stdout, "USAGE", "help must not masquerade as success output")
}

func TestUnknownSessionsSubcommandIsUsageError(t *testing.T) {
	_, _, exit, err := runCLIWithErr(t, "sessions", "frobnicate")
	require.Equal(t, 2, exit)
	env := usageEnvelope(t, err)
	require.Contains(t, env.Message, "unknown command")
	require.Contains(t, env.Message, "stem sessions --help")
}

// Rejecting unknown commands must not break the bare-invocation help
// paths: `stem` and `stem sessions` keep printing help, exit 0.
func TestBareRootPrintsHelp(t *testing.T) {
	stdout, _, exit, err := runCLIWithErr(t)
	require.NoError(t, err)
	require.Equal(t, 0, exit)
	require.Contains(t, stdout, "sessions")
}

func TestBareSessionsGroupPrintsHelp(t *testing.T) {
	stdout, _, exit, err := runCLIWithErr(t, "sessions")
	require.NoError(t, err)
	require.Equal(t, 0, exit)
	require.Contains(t, stdout, "list")
}

// Flag parse failures must keep the usage class and carry corrective
// guidance pointing at the failing leaf's help.
func TestUnknownFlagCarriesCorrectiveGuidance(t *testing.T) {
	_, _, exit, err := runCLIWithErr(t, "sessions", "list", "--bogus")
	require.Equal(t, 2, exit)
	env := usageEnvelope(t, err)
	require.Contains(t, env.Message, "unknown flag")
	require.Contains(t, env.Message, "stem sessions list --help")
}
