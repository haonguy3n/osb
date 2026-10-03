package apkindex

import (
	"strings"
	"unicode"
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
	register := func(token string, e *Entry) {
		if token == "" {
			return
		}
		if i := strings.IndexByte(token, '='); i >= 0 {
			token = token[:i]
		}
		if cur, ok := t.byName[token]; ok {
			if compareVersions(e.Version, cur.Version) > 0 {
				t.byName[token] = e
			}
			return
		}
		t.byName[token] = e
	}

	for i := range entries {
		e := &entries[i]
		register(e.Name, e)
		for _, p := range e.Provides {
			register(p, e)
		}
	}
	return t
}

func compareVersions(a, b string) int {
	if a == b {
		return 0
	}
	av, ar := splitRelease(a)
	bv, br := splitRelease(b)
	if c := comparePkgver(av, bv); c != 0 {
		return c
	}
	return compareInts(ar, br)
}

func splitRelease(v string) (pkgver string, release int) {
	i := strings.LastIndex(v, "-r")
	if i < 0 {
		return v, 0
	}
	tail := v[i+2:]
	if tail == "" || !allDigits(tail) {
		return v, 0
	}
	n := 0
	for _, c := range tail {
		n = n*10 + int(c-'0')
	}
	return v[:i], n
}

func comparePkgver(a, b string) int {
	as := splitVerSegments(a)
	bs := splitVerSegments(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := compareSegment(as[i], bs[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(as) < len(bs):
		if isPrereleaseSegment(bs[len(as)]) {
			return +1
		}
		return -1
	case len(as) > len(bs):
		if isPrereleaseSegment(as[len(bs)]) {
			return -1
		}
		return +1
	}
	return 0
}

func splitVerSegments(v string) []string {
	if v == "" {
		return nil
	}
	var out []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	var prevKind int
	for _, r := range v {
		switch {
		case r == '.' || r == '_' || r == '+' || r == '-':
			flush()
			if r == '_' {
				out = append(out, "_")
			}
			prevKind = 0
			continue
		case unicode.IsDigit(r):
			if prevKind == 2 {
				flush()
			}
			cur.WriteRune(r)
			prevKind = 1
		default:
			if prevKind == 1 {
				flush()
			}
			cur.WriteRune(r)
			prevKind = 2
		}
	}
	flush()
	return out
}

func compareSegment(a, b string) int {
	aPre := isPrereleaseSegment(a)
	bPre := isPrereleaseSegment(b)
	if aPre && !bPre {
		return -1
	}
	if bPre && !aPre {
		return +1
	}
	if allDigits(a) && allDigits(b) {
		return compareNumericString(a, b)
	}
	return strings.Compare(a, b)
}

func isPrereleaseSegment(s string) bool {
	switch s {
	case "alpha", "beta", "pre", "rc", "_":
		return true
	}
	if strings.HasPrefix(s, "alpha") || strings.HasPrefix(s, "beta") ||
		strings.HasPrefix(s, "pre") || strings.HasPrefix(s, "rc") {
		return true
	}
	return false
}

func compareNumericString(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return +1
	}
	return strings.Compare(a, b)
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return +1
	}
	return 0
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
