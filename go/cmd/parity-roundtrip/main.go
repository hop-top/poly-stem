// parity-roundtrip reads a crtx v0.1 envelope JSON file at argv[1],
// parses it into the Go SDK's Session type, then re-serializes it
// back to stdout as JSON. Used by tools/parity/runner.sh to verify
// wire-format parity across the 5 polyglot SDKs.
//
// No business logic — strictly parse + serialize round-trip via the
// public hop.top/stem API.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"hop.top/stem"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: parity-roundtrip <envelope.json>")
		os.Exit(2)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "parity-roundtrip: read %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	var env stem.Session
	if err := json.Unmarshal(data, &env); err != nil {
		fmt.Fprintf(os.Stderr, "parity-roundtrip: parse: %v\n", err)
		os.Exit(1)
	}

	out, err := json.Marshal(&env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parity-roundtrip: serialize: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintf(os.Stderr, "parity-roundtrip: write: %v\n", err)
		os.Exit(1)
	}
}
