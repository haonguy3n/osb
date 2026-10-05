package dpkg

import (
	"fmt"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

type Providers interface {
	Resolve(token string) (pkgName string, ok bool)
}

type TableProviders struct{ Table *ProvidesTable }

func MaterializeUnit(entry Entry, providers Providers, moduleName, distro string) (*osbstar.Unit, error) {
	if providers == nil {
		return nil, fmt.Errorf("dpkg: materialize %s: nil Providers", entry.Package)
	}

	depTokens, err := relationTokens(entry.PreDepends + ", " + entry.Depends)
	if err != nil {
		return nil, fmt.Errorf("dpkg: materialize %s: %w", entry.Package, err)
	}
	runtimeDeps := make([]string, 0, len(depTokens))
	seen := make(map[string]struct{}, len(depTokens))
	for _, t := range depTokens {
		pkg, ok := providers.Resolve(t)
		if !ok {
			continue
		}
		if pkg == entry.Package {
			continue
		}
		if _, dup := seen[pkg]; dup {
			continue
		}
		seen[pkg] = struct{}{}
		runtimeDeps = append(runtimeDeps, pkg)
	}

	provides, err := bareProvides(entry.Provides)
	if err != nil {
		return nil, fmt.Errorf("dpkg: materialize %s: provides: %w", entry.Package, err)
	}

	u := &osbstar.Unit{
		Name:        entry.Package,
		Class:       "unit",
		Description: entry.Description,
		Version:     entry.Version,
		RuntimeDeps: runtimeDeps,
		Provides:    provides,
		Module:      moduleName,
		Distro:      distro,
	}
	return u, nil
}

func relationTokens(line string) ([]string, error) {
	dep, err := ParseDependency(line)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(dep.Relations))
	for _, rel := range dep.Relations {
		for _, p := range rel.Possibilities {
			if p.Name == "" {
				continue
			}
			out = append(out, p.Name)
			break
		}
	}
	return out, nil
}

func bareProvides(line string) ([]string, error) {
	if line == "" {
		return nil, nil
	}
	provs, err := ParseProvides(line)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(provs))
	for _, p := range provs {
		if p.Name != "" {
			out = append(out, p.Name)
		}
	}
	return out, nil
}
