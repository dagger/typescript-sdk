package identifiers

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Names are names formatted by the engine's naming rules of one version,
// looked up while a generator renders, and formatted in batches by Run.
//
// A nil *Names is the legacy rules: Lookup finds nothing and Run renders
// once, so a generator falls back to its legacy conversions everywhere.
type Names struct {
	engine  Engine
	version string

	mu sync.Mutex
	// formatted holds the formatted names, by format and name.
	formatted map[Format]map[string]string
	// pending holds the names looked up but not formatted yet.
	pending map[Format]map[string]bool
}

// NewNames returns empty Names that formats names with the dictionary of
// version, through engine. Most generators get theirs from a Connector, which
// applies the gate first.
func NewNames(engine Engine, version string) *Names {
	return &Names{
		engine:    engine,
		version:   version,
		formatted: map[Format]map[string]string{},
		pending:   map[Format]map[string]bool{},
	}
}

// Version is the version whose dictionary names are parsed with, or "" for
// the legacy rules.
func (n *Names) Version() string {
	if n == nil {
		return ""
	}
	return n.version
}

// Lookup returns name formatted in f. When it isn't formatted yet, Lookup
// records it for the next batch and returns false, and so it does for a name
// the engine can't parse (see Valid), which no batch formats: either way the
// caller uses its fallback. A nil *Names always returns false.
func (n *Names) Lookup(f Format, name string) (string, bool) {
	if n == nil || !Valid(name) {
		return "", false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if formatted, ok := n.formatted[f][name]; ok {
		return formatted, true
	}
	if n.pending[f] == nil {
		n.pending[f] = map[string]bool{}
	}
	n.pending[f][name] = true
	return "", false
}

// Prefetch records names to format in the next batch, so that the first
// render can find them.
func (n *Names) Prefetch(f Format, names ...string) {
	for _, name := range names {
		n.Lookup(f, name)
	}
}

// maxRounds bounds Run. A render only looks up names derived from what it
// rendered before, so it settles in a round or two; more means a bug.
const maxRounds = 4

// Run calls render until every name it looks up is formatted. Before each
// render, the names recorded since the last batch are formatted in one
// round trip; a render that recorded nothing new is final. A render that
// fails while names were still missing is retried with them formatted, since
// a fallback name may be what it failed on.
//
// With a nil *Names, Run calls render once.
func (n *Names) Run(ctx context.Context, render func() error) error {
	return Run(ctx, render, n)
}

// Run is Names.Run for a render that looks names up in several Names, such as
// Go module bindings (in the Go SDK), whose identifiers follow one version and
// whose schema names another. Each round formats the names missing from all
// of them in a single round trip. Nil Names are the legacy rules and are
// skipped; with no others, render runs once.
func Run(ctx context.Context, render func() error, names ...*Names) error {
	var all []*Names
	for _, n := range names {
		if n != nil && !slices.Contains(all, n) {
			all = append(all, n)
		}
	}
	if len(all) == 0 {
		return render()
	}
	for round := 1; ; round++ {
		if err := format(ctx, all); err != nil {
			return err
		}
		err := render()
		var missing []string
		for _, n := range all {
			missing = append(missing, n.missing()...)
		}
		switch {
		case len(missing) == 0:
			return err
		case round == maxRounds:
			if err != nil {
				return err
			}
			return fmt.Errorf("identifiers: names still unformatted after %d rounds: %s", maxRounds, strings.Join(missing, ", "))
		}
	}
}

// missing lists the pending names, sorted, for an error.
func (n *Names) missing() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var missing []string
	for f, names := range n.pending {
		for name := range names {
			missing = append(missing, fmt.Sprintf("%q (%s, %s)", name, f.Casing, n.version))
		}
	}
	slices.Sort(missing)
	return missing
}

// format formats the pending names of every Names in a single query: one
// aliased formatIdentifiers field per Names and format, each at its Names'
// version. Every Names comes from the same engine.
func format(ctx context.Context, all []*Names) error {
	var requests []request
	var owners []*Names
	for _, n := range all {
		for _, r := range n.requests() {
			requests = append(requests, r)
			owners = append(owners, n)
		}
	}
	if len(requests) == 0 {
		return nil
	}
	formatted, err := formatIdentifiers(ctx, all[0].engine, requests)
	if err != nil {
		return err
	}
	for i, r := range requests {
		owners[i].store(r, formatted[i])
	}
	return nil
}

// requests returns the pending names as requests, one per format, sorted.
func (n *Names) requests() []request {
	n.mu.Lock()
	defer n.mu.Unlock()
	var requests []request
	for f, names := range n.pending {
		if len(names) == 0 {
			continue
		}
		requests = append(requests, request{format: f, version: n.version, names: slices.Sorted(maps.Keys(names))})
	}
	slices.SortFunc(requests, func(a, b request) int {
		return strings.Compare(string(a.format.Casing)+" "+string(a.format.Acronyms), string(b.format.Casing)+" "+string(b.format.Acronyms))
	})
	return requests
}

// store records the formatted names of a request.
func (n *Names) store(r request, formatted []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.formatted[r.format] == nil {
		n.formatted[r.format] = map[string]string{}
	}
	for i, name := range r.names {
		n.formatted[r.format][name] = formatted[i]
		delete(n.pending[r.format], name)
	}
}
