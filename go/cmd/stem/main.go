// stem is the hop.top CLI for the stem runtime: cross-CLI session
// lookup over crtx v0.1 envelopes (docs/specs/sessions-cli.md).
package main

import (
	"context"
	"os"

	"hop.top/stem/cmd/stem/commands"
)

// version is stamped via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	c := commands.New(version)
	err := c.Root.Execute(context.Background())
	// Semantic outcomes never surface as errors: the sessions leaves
	// render their own documents and record their exit code on the
	// CLI value. ResolveExit folds both paths into one status.
	os.Exit(c.ResolveExit(err))
}
