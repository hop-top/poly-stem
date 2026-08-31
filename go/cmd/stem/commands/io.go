package commands

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// Format contract values (spec §2): every sessions leaf declares
// --format text|json explicitly, default text.
const (
	formatText = "text"
	formatJSON = "json"
)

// Structured error codes and their exit codes (spec §2.7 / §3.6).
const (
	exitInternal  = 1
	exitUsage     = 2
	exitNotFound  = 3
	exitAmbiguous = 4
)

var errorCodes = map[int]string{
	exitInternal:  "internal",
	exitUsage:     "usage",
	exitNotFound:  "not_found",
	exitAmbiguous: "ambiguous_id",
}

// sink owns one invocation's output discipline: stdout carries only
// the result document, warnings buffer until success and then land
// on stderr, failures emit exactly one error document on stderr.
type sink struct {
	cmd      *cobra.Command
	format   string
	warnings []string
}

// newSink validates the leaf's --format value. On an invalid value
// the usage error is rendered in text (the requested format is not
// trustworthy) and the caller must stop.
func (c *CLI) newSink(cmd *cobra.Command, format string) (*sink, bool) {
	s := &sink{cmd: cmd, format: format}
	if format != formatText && format != formatJSON {
		s.format = formatText
		c.fail(s, exitUsage,
			fmt.Sprintf("invalid --format %q: use text or json", format),
			"every sessions leaf accepts --format text|json (default text)", nil)
		return nil, false
	}
	return s, true
}

func (s *sink) warnf(format string, args ...any) {
	s.warnings = append(s.warnings, fmt.Sprintf(format, args...))
}

// flushWarnings writes buffered notices to stderr. Called only on
// the success path so a failing json invocation leaves exactly one
// document on stderr.
func (s *sink) flushWarnings() {
	for _, w := range s.warnings {
		fmt.Fprintf(s.cmd.ErrOrStderr(), "stem: warning: %s\n", w)
	}
}

// errorDoc is the structured error document (spec §3.6), pinned by
// contracts/sessions/error.yaml.
type errorDoc struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Hint    string         `json:"hint,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// fail records the exit code and renders the error: in json, one
// structured document on stderr and nothing on stdout; in text, a
// human-readable line (plus hint) on stderr. Always returns nil so
// no outer layer renders a second document.
func (c *CLI) fail(s *sink, exit int, message, hint string, details map[string]any) error {
	c.exit = exit
	w := s.cmd.ErrOrStderr()
	if s.format == formatJSON {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(errorDoc{Error: errorBody{
			Code:    errorCodes[exit],
			Message: message,
			Hint:    hint,
			Details: details,
		}})
		return nil
	}
	fmt.Fprintf(w, "%s: %s\n", s.cmd.CommandPath(), message)
	if hint != "" {
		fmt.Fprintf(w, "hint: %s\n", hint)
	}
	return nil
}

// emitJSON writes the single result document to stdout with a
// trailing newline.
func (s *sink) emitJSON(doc any) error {
	enc := json.NewEncoder(s.cmd.OutOrStdout())
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}
