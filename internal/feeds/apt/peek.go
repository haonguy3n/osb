package apt

import (
	"fmt"
	"path/filepath"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type FeedDecl struct {
	Name      string
	Distro    string
	URL       string
	ArchURLs  map[string]string
	Suite     string
	Component string
	Arches    []string
	Index     string
	Keyring   string
}

func (d FeedDecl) baseURLFor(osbArch string) string {
	if u, ok := d.ArchURLs[osbArch]; ok && u != "" {
		return u
	}
	return d.URL
}

func PeekFeedDecls(modulePath string) ([]FeedDecl, error) {
	file := filepath.Join(modulePath, "MODULE.star")
	var (
		decls    []FeedDecl
		seenName = map[string]bool{}
	)

	noop := starlark.NewBuiltin("noop",
		func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			return starlark.None, nil
		})

	feed := starlark.NewBuiltin("apt_feed",
		func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			d := FeedDecl{}
			for _, kv := range kwargs {
				k, ok := kv[0].(starlark.String)
				if !ok {
					continue
				}
				switch string(k) {
				case "name":
					if v, ok := kv[1].(starlark.String); ok {
						d.Name = string(v)
					}
				case "distro":
					if v, ok := kv[1].(starlark.String); ok {
						d.Distro = string(v)
					}
				case "url":
					if v, ok := kv[1].(starlark.String); ok {
						d.URL = string(v)
					}
				case "arch_urls":
					if dict, ok := kv[1].(*starlark.Dict); ok {
						d.ArchURLs = stringDictFrom(dict)
					}
				case "suite":
					if v, ok := kv[1].(starlark.String); ok {
						d.Suite = string(v)
					}
				case "component":
					if v, ok := kv[1].(starlark.String); ok {
						d.Component = string(v)
					}
				case "arches":
					if list, ok := kv[1].(*starlark.List); ok {
						d.Arches = stringListFrom(list)
					}
				case "index":
					if v, ok := kv[1].(starlark.String); ok {
						d.Index = string(v)
					}
				case "keyring":
					if v, ok := kv[1].(starlark.String); ok {
						d.Keyring = string(v)
					}
				}
			}
			if d.Name == "" {
				return nil, fmt.Errorf("apt_feed: name is required")
			}
			if seenName[d.Name] {
				return nil, fmt.Errorf("apt_feed: duplicate feed name %q in this module", d.Name)
			}
			seenName[d.Name] = true
			decls = append(decls, d)
			return starlark.None, nil
		})

	thread := &starlark.Thread{Name: file}
	predeclared := starlark.StringDict{
		"module_info": noop,
		"module":      noop,
		"apt_feed":    feed,
		"alpine_feed": noop,
	}
	if _, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, file, nil, predeclared); err != nil {
		return nil, fmt.Errorf("apt: peek %s: %w", file, err)
	}
	sort.SliceStable(decls, func(i, j int) bool { return decls[i].Name < decls[j].Name })
	return decls, nil
}
