package dpkg

import (
	"strings"

	"pault.ag/go/debian/version"
)

type ProvidesTable struct {
	byName map[string]*Entry
}

func (p *ProvidesTable) Lookup(name string) *Entry {
	if p == nil {
		return nil
	}
	return p.byName[name]
}

func (p *ProvidesTable) Names() []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.byName))
	for n := range p.byName {
		out = append(out, n)
	}
	return out
}

func BuildProvidesTable(entries []Entry) *ProvidesTable {
	t := &ProvidesTable{byName: make(map[string]*Entry, len(entries)*2)}
	register := func(token, ver string, e *Entry) {
		token = strings.TrimSpace(token)
		if token == "" {
			return
		}
		if cur, ok := t.byName[token]; ok {
			if newerVersion(ver, cur.Version) {
				t.byName[token] = e
			}
			return
		}
		t.byName[token] = e
	}
	for i := range entries {
		e := &entries[i]
		register(e.Package, e.Version, e)
		if e.Provides == "" {
			continue
		}
		possibilities, err := ParseProvides(e.Provides)
		if err != nil {
			continue
		}
		for _, p := range possibilities {
			register(p.Name, e.Version, e)
		}
	}
	return t
}

func newerVersion(a, b string) bool {
	av, errA := version.Parse(a)
	bv, errB := version.Parse(b)
	if errA != nil || errB != nil {
		return a > b
	}
	return version.Compare(av, bv) > 0
}
