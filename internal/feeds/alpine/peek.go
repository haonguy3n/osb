package alpine

import (
	"fmt"
	"path/filepath"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

type FeedDecl struct {
	Name    string
	URL     string
	Branch  string
	Section string
	Index   string
	Keys    []string
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

	feed := starlark.NewBuiltin("alpine_feed",
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
				case "url":
					if v, ok := kv[1].(starlark.String); ok {
						d.URL = string(v)
					}
				case "branch":
					if v, ok := kv[1].(starlark.String); ok {
						d.Branch = string(v)
					}
				case "section":
					if v, ok := kv[1].(starlark.String); ok {
						d.Section = string(v)
					}
				case "index":
					if v, ok := kv[1].(starlark.String); ok {
						d.Index = string(v)
					}
				case "keys":
					if list, ok := kv[1].(*starlark.List); ok {
						d.Keys = stringListFrom(list)
					}
				}
			}
			if d.Name == "" {
				return nil, fmt.Errorf("alpine_feed: name is required")
			}
			if seenName[d.Name] {
				return nil, fmt.Errorf("alpine_feed: duplicate feed name %q in this module", d.Name)
			}
			seenName[d.Name] = true
			decls = append(decls, d)
			return starlark.None, nil
		})

	thread := &starlark.Thread{Name: file}
	predeclared := starlark.StringDict{
		"module_info": noop,
		"module":      noop,
		"alpine_feed": feed,
	}
	if _, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, file, nil, predeclared); err != nil {
		return nil, fmt.Errorf("alpine: peek %s: %w", file, err)
	}
	sort.SliceStable(decls, func(i, j int) bool { return decls[i].Name < decls[j].Name })
	return decls, nil
}
