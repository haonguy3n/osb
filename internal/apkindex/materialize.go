package apkindex

import (
	"fmt"
	"strings"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

type Providers interface {
	Resolve(token string) (pkgName string, ok bool)
}

func MaterializeUnit(entry Entry, providers Providers, moduleName string) (*osbstar.Unit, error) {
	deps, err := resolveRuntimeDeps(entry, providers)
	if err != nil {
		return nil, fmt.Errorf("apkindex: materialize %s: %w", entry.Name, err)
	}

	u := &osbstar.Unit{
		Name:           entry.Name,
		Class:          "unit",
		Description:    entry.Description,
		License:        entry.License,
		APKChecksum:    entry.ChecksumText,
		RuntimeDeps:    deps,
		Provides:       filterProvides(entry.Provides),
		Replaces:       filterProvides(entry.Replaces),
		Module:         moduleName,
		PassthroughAPK: "",
	}

	u.Version, u.Release = splitPkgver(entry.Version)

	return u, nil
}

func resolveRuntimeDeps(entry Entry, providers Providers) ([]string, error) {
	if providers == nil {
		return nil, fmt.Errorf("nil Providers")
	}
	var (
		out  []string
		seen = make(map[string]struct{}, len(entry.Deps))
	)
	for _, raw := range entry.Deps {
		d, err := ParseDep(raw)
		if err != nil {
			return nil, fmt.Errorf("dep %q: %w", raw, err)
		}
		if d.Conflict {
			continue
		}
		if d.Kind == DepKindPath {
			continue
		}
		if d.Kind == DepKindName && d.Name == entry.Name {
			continue
		}
		pkg, ok := providers.Resolve(d.Name)
		if !ok {
			return nil, fmt.Errorf("unresolved dep %q (no provider for %q)",
				raw, d.Name)
		}
		if pkg == entry.Name {
			continue
		}
		if _, dup := seen[pkg]; dup {
			continue
		}
		seen[pkg] = struct{}{}
		out = append(out, pkg)
	}
	return out, nil
}

func filterProvides(provides []string) []string {
	if len(provides) == 0 {
		return nil
	}
	out := make([]string, 0, len(provides))
	for _, p := range provides {
		if strings.HasPrefix(p, "so:") || strings.HasPrefix(p, "cmd:") {
			continue
		}
		if i := strings.IndexByte(p, '='); i >= 0 {
			p = p[:i]
		}
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func splitPkgver(pkgver string) (string, int) {
	i := strings.LastIndex(pkgver, "-r")
	if i < 0 {
		return pkgver, 0
	}
	tail := pkgver[i+2:]
	if tail == "" {
		return pkgver, 0
	}
	n := 0
	for _, c := range tail {
		if c < '0' || c > '9' {
			return pkgver, 0
		}
		n = n*10 + int(c-'0')
	}
	return pkgver[:i], n
}
