package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	"hop.top/stem/adapters/claudecode"
	"hop.top/stem/adapters/codex"
	"hop.top/stem/adapters/copilot"
	"hop.top/stem/sessions"
)

// Environment overrides for the scanned roots. Colon-separated
// directory lists; empty entries are dropped. STEM_CRTX_DIRS is
// specified by the sessions spec (§1.1) and replaces the default
// crtx root list entirely; the adapter variables mirror it for the
// native stores (tests and unusual hosts).
const (
	envCrtxDirs       = "STEM_CRTX_DIRS"
	envClaudeCodeDirs = "STEM_CLAUDECODE_DIRS"
	envCodexDirs      = "STEM_CODEX_DIRS"
	envCopilotDirs    = "STEM_COPILOT_DIRS"
)

// crtxProducers are the XDG state subdirectories scanned for
// crtx-native session files (spec §1.1).
var crtxProducers = []string{"crtx", "stem", "nerv"}

// defaultLimit caps list/search results when --limit is not given
// (spec §2.2).
const defaultLimit = 20

func (c *CLI) sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Cross-CLI session lookup over crtx envelopes",
		Long: `Recall sessions recorded by any crtx-producing CLI: enumerate,
inspect, search, and walk fork lineage across the crtx session roots
and the native Claude Code, Codex, and Copilot stores.`,
		// Runnable group: bare `stem sessions` prints help (exit 0),
		// unmatched subcommands reject as usage errors (spec §2.7).
		RunE:        runHelp,
		Args:        rejectUnknownSubcommand,
		Annotations: map[string]string{"kit/top-level-verb": "true"},
	}
	markReadLeaf(cmd)
	cmd.AddCommand(c.sessionsListCmd())
	cmd.AddCommand(c.sessionsShowCmd())
	cmd.AddCommand(c.sessionsSearchCmd())
	cmd.AddCommand(c.sessionsLineageCmd())
	return cmd
}

// filterFlags carries the shared recall-axis flags (spec §2.2).
type filterFlags struct {
	since   string
	until   string
	cwd     string
	sources []string
	limit   int
}

func addFilterFlags(cmd *cobra.Command, ff *filterFlags) {
	fl := cmd.Flags()
	fl.StringVar(&ff.since, "since", "",
		"Keep sessions whose activity intersects the window from this time (RFC 3339, YYYY-MM-DD, or <N>h/<N>d/<N>w)")
	fl.StringVar(&ff.until, "until", "",
		"Upper bound of the time window (same forms; default now)")
	fl.StringVar(&ff.cwd, "cwd", "",
		"Keep sessions whose working directory equals this path or lies under it")
	fl.StringArrayVar(&ff.sources, "source", nil,
		"Keep sessions whose source kind matches (repeatable)")
	fl.IntVar(&ff.limit, "limit", defaultLimit,
		"Cap result count after sorting (0 = unlimited)")
}

// build validates the flag values into a sessions.Filter.
func (ff *filterFlags) build(now time.Time) (sessions.Filter, error) {
	var f sessions.Filter
	if ff.since != "" {
		t, err := sessions.ParseWhen(ff.since, now)
		if err != nil {
			return f, fmt.Errorf("--since: %w", err)
		}
		f.Since = t
	}
	if ff.until != "" {
		t, err := sessions.ParseWhen(ff.until, now)
		if err != nil {
			return f, fmt.Errorf("--until: %w", err)
		}
		f.Until = t
	}
	if ff.cwd != "" {
		abs, err := filepath.Abs(ff.cwd)
		if err != nil {
			return f, fmt.Errorf("--cwd: %v", err)
		}
		f.CWD = abs
	}
	if ff.limit < 0 {
		return f, fmt.Errorf("--limit: must be >= 0")
	}
	f.Sources = ff.sources
	return f, nil
}

// scan discovers the full session set across crtx roots and every
// native-store adapter, honoring the environment overrides.
func scan(s *sink) []*sessions.Record {
	return sessions.Scan(sessions.Options{
		CrtxRoots: dirsFromEnv(envCrtxDirs, defaultCrtxRoots),
		Sources: []sessions.Source{
			{Adapter: claudecode.Adapter{}, Roots: dirsFromEnv(envClaudeCodeDirs, claudecode.Adapter{}.DefaultRoots)},
			{Adapter: codex.Adapter{}, Roots: dirsFromEnv(envCodexDirs, codex.Adapter{}.DefaultRoots)},
			{Adapter: copilot.Adapter{}, Roots: dirsFromEnv(envCopilotDirs, copilot.Adapter{}.DefaultRoots)},
		},
		Warn: func(msg string) { s.warnf("%s", msg) },
	})
}

func dirsFromEnv(key string, fallback func() []string) []string {
	if v, ok := os.LookupEnv(key); ok {
		var dirs []string
		for _, d := range strings.Split(v, ":") {
			if d != "" {
				dirs = append(dirs, d)
			}
		}
		return dirs
	}
	return fallback()
}

// defaultCrtxRoots resolves $XDG_STATE_HOME/<producer>/sessions for
// the crtx-native producers (default state home ~/.local/state).
func defaultCrtxRoots() []string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		base = filepath.Join(home, ".local", "state")
	}
	roots := make([]string, 0, len(crtxProducers))
	for _, p := range crtxProducers {
		roots = append(roots, filepath.Join(base, p, "sessions"))
	}
	return roots
}

// truncateRecords applies --limit and reports whether it cut the set.
func truncateRecords(recs []*sessions.Record, limit int) ([]*sessions.Record, bool) {
	if limit <= 0 || len(recs) <= limit {
		return recs, false
	}
	return recs[:limit], true
}

// summarize maps records to the shared SessionSummary shape,
// guaranteeing a non-nil slice for stable JSON.
func summarize(recs []*sessions.Record) []sessions.Summary {
	out := make([]sessions.Summary, 0, len(recs))
	for _, r := range recs {
		out = append(out, sessions.Summarize(r))
	}
	return out
}

// markReadLeaf stamps the kit annotations every sessions leaf
// shares: side-effect-free reads, naturally idempotent.
func markReadLeaf(cmd *cobra.Command) {
	kitcli.SetSideEffect(cmd, kitcli.SideEffectRead)
	kitcli.SetIdempotency(cmd, kitcli.IdempotencyYes)
}

// resolveRef addresses one session by full id or prefix (spec §2.1)
// for the id-taking leaves, rendering the structured error and
// recording the exit code on failure; ok is false when the caller
// must stop.
func (c *CLI) resolveRef(s *sink, recs []*sessions.Record, ref string) (rec *sessions.Record, ok bool) {
	rec, candidates, err := sessions.Resolve(recs, ref)
	switch {
	case errors.Is(err, sessions.ErrRefTooShort):
		_ = c.fail(s, exitUsage,
			fmt.Sprintf("id prefix %q is shorter than %d characters", ref, sessions.MinRefLength),
			"supply the full id or a longer prefix", nil)
		return nil, false
	case errors.Is(err, sessions.ErrNotFound):
		_ = c.fail(s, exitNotFound,
			fmt.Sprintf("no session matches id %q", ref),
			"run `stem sessions list` to see known sessions", nil)
		return nil, false
	case errors.Is(err, sessions.ErrAmbiguous):
		if len(candidates) > maxCandidates {
			candidates = candidates[:maxCandidates]
		}
		_ = c.fail(s, exitAmbiguous,
			fmt.Sprintf("prefix %q matches %d sessions", ref, len(candidates)),
			"retry with a longer prefix",
			map[string]any{"candidates": summarize(candidates)})
		return nil, false
	case err != nil:
		_ = c.fail(s, exitInternal, err.Error(), "", nil)
		return nil, false
	}
	return rec, true
}
