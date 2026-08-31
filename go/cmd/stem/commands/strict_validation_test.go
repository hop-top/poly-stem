package commands_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kitconformance "hop.top/kit/go/conformance"
	kitcli "hop.top/kit/go/console/cli"

	"hop.top/stem/cmd/stem/commands"
)

// TestKitStrictGates locks in kit's Layer-A contract for the stem
// command tree plus the sessions-spec format discipline: every
// sessions leaf declares its own `--format text|json` contract
// (never a bare inherited --format) and is a read-only, idempotent
// leaf.
func TestKitStrictGates(t *testing.T) {
	c := commands.New("test")

	require.NoError(t, c.Root.Validate(), "Layer-A validation must pass")
	if ve := kitconformance.AssertCLI(t, c.Root); ve != nil {
		t.Fatalf("kit conformance: %v", ve)
	}

	report := c.Root.ValidateSignature()
	require.False(t, report.HasViolations(),
		"signature validation must pass: %+v", report.Violations)

	var found bool
	for _, sub := range c.Root.Cmd.Commands() {
		if sub.Name() != "sessions" {
			continue
		}
		found = true
		require.NotEmpty(t, sub.Commands(), "sessions group must own leaves")
		for _, leaf := range sub.Commands() {
			if !leaf.Runnable() {
				continue
			}
			f := leaf.LocalFlags().Lookup("format")
			require.NotNil(t, f, "%s must declare its own --format contract", leaf.CommandPath())
			assert.Equal(t, "text", f.DefValue, "%s: --format defaults to text", leaf.CommandPath())

			se, ok := kitcli.GetSideEffect(leaf)
			require.True(t, ok, "%s missing kit/side-effect", leaf.CommandPath())
			assert.Equal(t, kitcli.SideEffectRead, se, "%s must be read-only", leaf.CommandPath())
		}
	}
	require.True(t, found, "sessions group must be mounted on the root")
}
