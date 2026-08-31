package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"hop.top/stem/cmd/stem/commands"
)

// runCLI executes one in-process invocation against a fresh command
// tree and returns captured stdout, stderr, and the resolved exit
// code.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	c := commands.New("test")
	var out, errb bytes.Buffer
	c.Root.Cmd.SetOut(&out)
	c.Root.Cmd.SetErr(&errb)
	c.Root.Cmd.SetArgs(args)
	err := c.Root.Cmd.ExecuteContext(context.Background())
	return out.String(), errb.String(), c.ResolveExit(err)
}

// fixtureEnv points every scan root at committed synthetic fixtures
// so no test ever touches a real user store.
func fixtureEnv(t *testing.T) {
	t.Helper()
	crtx, err := filepath.Abs(filepath.Join("testdata", "crtx"))
	require.NoError(t, err)
	cc, err := filepath.Abs(filepath.Join("..", "..", "..", "adapters", "claudecode", "testdata", "projects"))
	require.NoError(t, err)
	cx, err := filepath.Abs(filepath.Join("..", "..", "..", "adapters", "codex", "testdata", "store"))
	require.NoError(t, err)
	cp, err := filepath.Abs(filepath.Join("testdata", "copilot"))
	require.NoError(t, err)
	t.Setenv("STEM_CRTX_DIRS",
		filepath.Join(crtx, "stem", "sessions")+":"+filepath.Join(crtx, "nerv", "sessions"))
	t.Setenv("STEM_CLAUDECODE_DIRS", cc)
	t.Setenv("STEM_CODEX_DIRS", cx)
	t.Setenv("STEM_COPILOT_DIRS", cp)
}

// contractSchema compiles the inline JSON Schema of one eva contract
// under contracts/sessions/.
func contractSchema(t *testing.T, contract string) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "sessions", contract))
	require.NoError(t, err, "read contract %s", contract)

	var doc struct {
		Evaluators []struct {
			Name   string         `yaml:"name"`
			Schema map[string]any `yaml:"schema"`
		} `yaml:"evaluators"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Evaluators, "%s: no evaluators", contract)
	require.Equal(t, "json_schema_valid", doc.Evaluators[0].Name)

	js, err := json.Marshal(doc.Evaluators[0].Schema)
	require.NoError(t, err)

	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	c.AssertFormat = true
	require.NoError(t, c.AddResource("contract.json", bytes.NewReader(js)))
	sch, err := c.Compile("contract.json")
	require.NoError(t, err, "compile %s", contract)
	return sch
}

// assertContract validates a captured JSON document against the named
// eva contract schema and returns the decoded document.
func assertContract(t *testing.T, contract, doc string) map[string]any {
	t.Helper()
	require.NotEmpty(t, strings.TrimSpace(doc), "no JSON document captured")
	var v any
	require.NoError(t, json.Unmarshal([]byte(doc), &v), "output is not one JSON document: %q", doc)
	require.NoError(t, contractSchema(t, contract).Validate(v),
		"output violates %s", contract)
	m, ok := v.(map[string]any)
	require.True(t, ok, "document is not an object")
	return m
}

// sessionIDs extracts the id of every session summary in a list
// document, in order.
func sessionIDs(t *testing.T, doc map[string]any) []string {
	t.Helper()
	raw, ok := doc["sessions"].([]any)
	require.True(t, ok, "sessions array missing")
	ids := make([]string, 0, len(raw))
	for _, s := range raw {
		ids = append(ids, s.(map[string]any)["id"].(string))
	}
	return ids
}

// findSession returns the summary with the given id, or nil.
func findSession(doc map[string]any, id string) map[string]any {
	raw, _ := doc["sessions"].([]any)
	for _, s := range raw {
		m := s.(map[string]any)
		if m["id"] == id {
			return m
		}
	}
	return nil
}
