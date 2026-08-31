// Package sessions implements the discovery and query layer behind
// the `stem sessions` command group (docs/specs/sessions-cli.md):
// scanning crtx-native session roots and native-store adapters into
// one deduplicated, newest-first session set, plus the id-prefix
// resolution, filtering, and summarization the CLI leaves share.
//
// Everything here is read-only over the underlying stores.
package sessions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	stem "hop.top/stem"
	"hop.top/stem/adapters"
)

// crtxAdapterName is the StoreRef.Adapter value for envelopes read
// as-is from crtx session roots (spec §3.1).
const crtxAdapterName = "crtx"

// maxEnvelopeLine bounds a single crtx envelope line during root
// scanning (16 MiB).
const maxEnvelopeLine = 16 << 20

// StoreRef is the provenance of one discovered session: which
// adapter produced it and from which file. Adapter is "crtx" for
// crtx-native envelopes; Path is the envelope file itself in that
// case, the native store file otherwise.
type StoreRef struct {
	Adapter string `json:"adapter"`
	Path    string `json:"path"`
}

// Record is one discovered session.
type Record struct {
	// Envelope is the normalized crtx v0.1 envelope. Never nil.
	Envelope *stem.Session
	// Raw is the authoritative stored line for crtx-native sessions,
	// emitted verbatim by `show --format json`. Nil for
	// adapter-normalized sessions.
	Raw []byte
	// Store is the session's provenance.
	Store StoreRef
}

// Source pairs an adapter with the store roots to scan.
type Source struct {
	Adapter adapters.Adapter
	Roots   []string
}

// Options configures a Scan.
type Options struct {
	// CrtxRoots are directories holding crtx-native session files
	// (<id>.jsonl, one envelope per line, last non-empty line
	// authoritative; subdirectories one level deep are scanned too).
	CrtxRoots []string
	// Sources are the native-store adapters and their roots.
	Sources []Source
	// Warn receives non-fatal scan notices. Nil drops them.
	Warn func(msg string)
}

// Scan discovers every session across the configured roots and
// adapters, deduplicates by envelope id (crtx-native beats
// adapter-normalized; within a class the most recently updated
// wins), and returns the set sorted newest first. Missing roots
// yield nothing; per-file failures surface through Warn and never
// abort the scan.
func Scan(opts Options) []*Record {
	warn := opts.Warn
	if warn == nil {
		warn = func(string) {}
	}

	type slot struct {
		rec    *Record
		native bool
	}
	byID := make(map[string]slot)
	add := func(rec *Record, native bool) {
		id := rec.Envelope.ID
		if id == "" {
			warn(fmt.Sprintf("%s: envelope has no id; skipped", rec.Store.Path))
			return
		}
		cur, ok := byID[id]
		if !ok {
			byID[id] = slot{rec, native}
			return
		}
		switch {
		case native && !cur.native:
			byID[id] = slot{rec, native}
		case native == cur.native &&
			rec.Envelope.UpdatedAt.After(cur.rec.Envelope.UpdatedAt):
			byID[id] = slot{rec, native}
		}
	}

	// Adapters scan first so the crtx-native preference below is the
	// branch that decides duplicates, independent of discovery order.
	for _, src := range opts.Sources {
		for _, root := range src.Roots {
			for res, err := range src.Adapter.Scan(root) {
				if err != nil {
					warn(fmt.Sprintf("%s: %v", src.Adapter.Kind(), err))
					continue
				}
				add(&Record{
					Envelope: res.Envelope,
					Store: StoreRef{
						Adapter: src.Adapter.Kind(),
						Path:    metaString(res.Envelope, adapters.MetaNativePath),
					},
				}, false)
			}
		}
	}
	for _, root := range opts.CrtxRoots {
		scanCrtxRoot(root, func(r *Record) { add(r, true) }, warn)
	}

	out := make([]*Record, 0, len(byID))
	for _, s := range byID {
		out = append(out, s.rec)
	}
	Sort(out)
	return out
}

// scanCrtxRoot loads every session file directly under root and in
// its immediate subdirectories (spec §1.1: one level deep, no more).
func scanCrtxRoot(root string, add func(*Record), warn func(string)) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			warn(fmt.Sprintf("%s: %v", root, err))
		}
		return
	}
	loadDir := func(dir string, files []os.DirEntry) {
		for _, e := range files {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			rec, err := loadCrtxFile(path)
			if err != nil {
				warn(fmt.Sprintf("%s: %v", path, err))
				continue
			}
			add(rec)
		}
	}
	loadDir(root, entries)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(root, e.Name())
		subEntries, err := os.ReadDir(sub)
		if err != nil {
			warn(fmt.Sprintf("%s: %v", sub, err))
			continue
		}
		loadDir(sub, subEntries)
	}
}

// loadCrtxFile reads a crtx session file: the last non-empty line is
// the authoritative envelope (tolerates streaming appenders).
func loadCrtxFile(path string) (*Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var last []byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxEnvelopeLine)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			last = []byte(line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(last) == 0 {
		return nil, fmt.Errorf("no envelope line")
	}

	var env stem.Session
	if err := json.Unmarshal(last, &env); err != nil {
		return nil, fmt.Errorf("not a crtx envelope: %w", err)
	}
	if env.CrtxVersion != stem.CrtxVersion {
		return nil, fmt.Errorf("crtx_version %q is not %q; skipped", env.CrtxVersion, stem.CrtxVersion)
	}
	return &Record{
		Envelope: &env,
		Raw:      last,
		Store:    StoreRef{Adapter: crtxAdapterName, Path: path},
	}, nil
}

// metaString reads a string-valued envelope metadata key; empty when
// absent or not a string.
func metaString(env *stem.Session, key string) string {
	if env == nil || env.Metadata == nil {
		return ""
	}
	s, _ := env.Metadata[key].(string)
	return s
}
