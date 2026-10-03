package dpkg

import (
	"fmt"
	"strings"

	"pault.ag/go/debian/dependency"
)

type Op string

const (
	OpNone Op = ""
	OpLt   Op = "<<"
	OpLe   Op = "<="
	OpEq   Op = "="
	OpGe   Op = ">="
	OpGt   Op = ">>"
)

type Possibility struct {
	Name    string
	Arch    string
	Version string
	Op      Op

	Raw string
}

type Relation struct {
	Possibilities []Possibility
}

type Dependency struct {
	Relations []Relation
}

func ParseDependency(s string) (Dependency, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Dependency{}, nil
	}
	parsed, err := dependency.Parse(s)
	if err != nil {
		return Dependency{}, fmt.Errorf("dpkg: parse dependency %q: %w", s, err)
	}
	out := Dependency{Relations: make([]Relation, 0, len(parsed.Relations))}
	for _, rel := range parsed.Relations {
		rOut := Relation{Possibilities: make([]Possibility, 0, len(rel.Possibilities))}
		for _, p := range rel.Possibilities {
			pp := Possibility{
				Name: p.Name,
				Raw:  p.Name,
			}
			if p.Arch != nil {
				pp.Arch = p.Arch.String()
			}
			if p.Version != nil {
				pp.Version = p.Version.Number
				pp.Op = Op(p.Version.Operator)
			}
			rOut.Possibilities = append(rOut.Possibilities, pp)
		}
		out.Relations = append(out.Relations, rOut)
	}
	return out, nil
}

func ParseProvides(s string) ([]Possibility, error) {
	dep, err := ParseDependency(s)
	if err != nil {
		return nil, err
	}
	if len(dep.Relations) == 0 {
		return nil, nil
	}
	out := make([]Possibility, 0, len(dep.Relations))
	for _, rel := range dep.Relations {
		for _, p := range rel.Possibilities {
			out = append(out, p)
		}
	}
	return out, nil
}
