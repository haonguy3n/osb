package apkindex

import (
	"fmt"
	"strings"
)

type DepKind int

const (
	DepKindUnknown DepKind = iota
	DepKindName
	DepKindSo
	DepKindCmd
	DepKindPc
	DepKindPath
	DepKindConflict
)

type Op int

const (
	OpNone Op = iota
	OpEq
	OpLt
	OpLe
	OpGt
	OpGe
	OpTilde
)

type Dep struct {
	Kind    DepKind
	Name    string
	Version string
	Op      Op

	Conflict bool

	Raw string
}

func ParseDep(s string) (Dep, error) {
	if s == "" {
		return Dep{}, fmt.Errorf("apkindex: empty dep token")
	}
	d := Dep{Raw: s}
	if s[0] == '!' {
		d.Conflict = true
		s = s[1:]
		if s == "" {
			return Dep{}, fmt.Errorf("apkindex: dep %q: empty after !", d.Raw)
		}
	}
	if s[0] == '/' {
		d.Kind = DepKindPath
		d.Name = s
		return d, nil
	}

	name, op, ver := splitConstraint(s)
	if name == "" {
		return Dep{}, fmt.Errorf("apkindex: dep %q: empty name", d.Raw)
	}
	d.Op = op
	d.Version = ver

	switch {
	case strings.HasPrefix(name, "so:"):
		d.Kind = DepKindSo
	case strings.HasPrefix(name, "cmd:"):
		d.Kind = DepKindCmd
	case strings.HasPrefix(name, "pc:"):
		d.Kind = DepKindPc
	default:
		d.Kind = DepKindName
	}
	d.Name = name
	return d, nil
}

func splitConstraint(s string) (name string, op Op, version string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '=':
			return s[:i], OpEq, s[i+1:]
		case '~':
			return s[:i], OpTilde, s[i+1:]
		case '<':
			if i+1 < len(s) && s[i+1] == '=' {
				return s[:i], OpLe, s[i+2:]
			}
			return s[:i], OpLt, s[i+1:]
		case '>':
			if i+1 < len(s) && s[i+1] == '=' {
				return s[:i], OpGe, s[i+2:]
			}
			return s[:i], OpGt, s[i+1:]
		}
	}
	return s, OpNone, ""
}
