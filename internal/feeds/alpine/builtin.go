package alpine

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"go.starlark.net/starlark"

	"github.com/anhhao17/osb/internal/apkindex"
	osbstar "github.com/anhhao17/osb/internal/starlark"
)

var (
	engineFeedsMu sync.Mutex
	engineFeeds   = map[*osbstar.Engine][]*archState{}
)

func registerFeedState(eng *osbstar.Engine, s *archState) {
	engineFeedsMu.Lock()
	defer engineFeedsMu.Unlock()
	engineFeeds[eng] = append(engineFeeds[eng], s)
}

func feedStatesFor(eng *osbstar.Engine) []*archState {
	engineFeedsMu.Lock()
	defer engineFeedsMu.Unlock()
	src := engineFeeds[eng]
	out := make([]*archState, len(src))
	copy(out, src)
	return out
}

var archMap = map[string]string{
	"x86_64":  "x86_64",
	"arm64":   "aarch64",
	"riscv64": "riscv64",
}

func Builtin(eng *osbstar.Engine) *starlark.Builtin {
	return starlark.NewBuiltin("alpine_feed", makeAlpineFeed(eng))
}

func makeAlpineFeed(eng *osbstar.Engine) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		args, err := parseKwargs(kwargs)
		if err != nil {
			return nil, fmt.Errorf("alpine_feed: %w", err)
		}

		parent := eng.CurrentModule()
		if parent == "" {
			return nil, fmt.Errorf("alpine_feed: must be called from a module's MODULE.star (not the project root)")
		}
		composedName := parent + "." + args.name

		var moduleDir string
		if thread.CallStackDepth() >= 2 {
			if caller := thread.CallFrame(1).Pos.Filename(); caller != "" && caller != "<builtin>" {
				moduleDir = filepath.Dir(caller)
			}
		}
		indexRoot := args.index
		if !filepath.IsAbs(indexRoot) {
			indexRoot = filepath.Join(moduleDir, indexRoot)
		}

		sm := buildSyntheticModule(eng, composedName, parent, indexRoot, args)
		if err := eng.RegisterSyntheticModule(sm); err != nil {
			return nil, fmt.Errorf("alpine_feed %q: %w", composedName, err)
		}
		return starlark.None, nil
	}
}

func buildSyntheticModule(eng *osbstar.Engine, composedName, parent, indexRoot string, args alpineFeedArgs) *osbstar.SyntheticModule {
	s := &archState{
		indexRoot: indexRoot,
		eng:       eng,
		byArch:    make(map[string]*archCache),
		feedArgs:  args,
	}
	registerFeedState(eng, s)

	return &osbstar.SyntheticModule{
		Name:    composedName,
		Parent:  parent,
		Distro:  "alpine",
		Release: args.branch,
		Lookup: func(name string) (*osbstar.Unit, error) {
			return s.lookup(composedName, name)
		},
		Names: func() []string {
			return s.names()
		},
	}
}

type archState struct {
	indexRoot string
	eng       *osbstar.Engine
	byArch    map[string]*archCache
	feedArgs  alpineFeedArgs
}

type archCache struct {
	entries  []apkindex.Entry
	provides *apkindex.ProvidesTable
	byName   map[string]*apkindex.Entry
}

func (s *archState) cacheFor(arch string) (*archCache, error) {
	if c, ok := s.byArch[arch]; ok {
		return c, nil
	}
	alpineArch, ok := archMap[arch]
	if !ok {
		return nil, fmt.Errorf("alpine_feed: unsupported arch %q (supported: %s)",
			arch, strings.Join(supportedArches(), ", "))
	}
	indexPath := filepath.Join(s.indexRoot, alpineArch, "APKINDEX")
	entries, err := apkindex.ParseIndexFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("alpine_feed: load %s: %w", indexPath, err)
	}
	table := apkindex.BuildProvidesTable(entries)
	byName := make(map[string]*apkindex.Entry, len(entries))
	for i := range entries {
		byName[entries[i].Name] = &entries[i]
	}
	c := &archCache{entries: entries, provides: table, byName: byName}
	s.byArch[arch] = c
	return c, nil
}

func (s *archState) lookup(moduleName, name string) (*osbstar.Unit, error) {
	arch := s.eng.ActiveArch()
	if arch == "" {
		return nil, fmt.Errorf("alpine_feed: no active arch (machine not loaded?)")
	}
	c, err := s.cacheFor(arch)
	if err != nil {
		return nil, err
	}
	entry, ok := c.byName[name]
	if !ok {
		return nil, nil
	}
	providers := newMultiFeedProviders(s.eng, arch, c.provides)
	u, err := apkindex.MaterializeUnit(*entry, providers, moduleName)
	if err != nil {
		return nil, err
	}
	s.populateBuildFields(u, entry, arch)
	return u, nil
}

func (s *archState) populateBuildFields(u *osbstar.Unit, entry *apkindex.Entry, arch string) {
	alpineArch := archMap[arch]
	asset := fmt.Sprintf("%s-%s.apk", entry.Name, entry.Version)
	u.Source = fmt.Sprintf("%s/%s/%s/%s/%s",
		strings.TrimSuffix(s.feedArgs.url, "/"),
		s.feedArgs.branch,
		s.feedArgs.section,
		alpineArch,
		asset)
	u.PassthroughAPK = asset
	u.Container = "toolchain-musl"
	u.ContainerArch = "target"
	u.Sandbox = false
	u.Distro = "alpine"
	u.Tasks = []osbstar.Task{
		{
			Name: "install",
			Steps: []osbstar.Step{
				{Command: "mkdir -p $DESTDIR"},
				{Command: "tar -xzpf ./" + asset + " -C $DESTDIR " +
					"--exclude=.PKGINFO " +
					"--exclude=.pre-install --exclude=.post-install " +
					"--exclude=.pre-upgrade --exclude=.post-upgrade " +
					"--exclude=.pre-deinstall --exclude=.post-deinstall " +
					"--exclude=.trigger " +
					"--exclude=.SIGN.*"},
			},
		},
	}
}

type multiFeedProviders struct {
	primary  *apkindex.ProvidesTable
	siblings []*apkindex.ProvidesTable
}

func newMultiFeedProviders(eng *osbstar.Engine, arch string, primary *apkindex.ProvidesTable) multiFeedProviders {
	out := multiFeedProviders{primary: primary}
	for _, sibling := range feedStatesFor(eng) {
		if sibling.provides(arch) == primary {
			continue
		}
		if t := sibling.provides(arch); t != nil {
			out.siblings = append(out.siblings, t)
		}
	}
	return out
}

func (m multiFeedProviders) Resolve(token string) (string, bool) {
	if e := m.primary.Lookup(token); e != nil {
		return e.Name, true
	}
	for _, t := range m.siblings {
		if e := t.Lookup(token); e != nil {
			return e.Name, true
		}
	}
	return "", false
}

func (s *archState) provides(arch string) *apkindex.ProvidesTable {
	c, err := s.cacheFor(arch)
	if err != nil {
		return nil
	}
	return c.provides
}

func (s *archState) names() []string {
	arch := s.eng.ActiveArch()
	if arch == "" {
		return nil
	}
	c, err := s.cacheFor(arch)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(c.entries))
	for i := range c.entries {
		out = append(out, c.entries[i].Name)
	}
	return out
}

type alpineFeedArgs struct {
	name    string
	url     string
	branch  string
	section string
	index   string
	keys    []string
}

func parseKwargs(kwargs []starlark.Tuple) (alpineFeedArgs, error) {
	var a alpineFeedArgs
	for _, kv := range kwargs {
		k, ok := kv[0].(starlark.String)
		if !ok {
			continue
		}
		switch string(k) {
		case "name":
			if v, ok := kv[1].(starlark.String); ok {
				a.name = string(v)
			}
		case "url":
			if v, ok := kv[1].(starlark.String); ok {
				a.url = string(v)
			}
		case "branch":
			if v, ok := kv[1].(starlark.String); ok {
				a.branch = string(v)
			}
		case "section":
			if v, ok := kv[1].(starlark.String); ok {
				a.section = string(v)
			}
		case "index":
			if v, ok := kv[1].(starlark.String); ok {
				a.index = string(v)
			}
		case "keys":
			if list, ok := kv[1].(*starlark.List); ok {
				a.keys = stringListFrom(list)
			}
		}
	}
	if a.name == "" {
		return a, fmt.Errorf("name is required")
	}
	if a.url == "" {
		return a, fmt.Errorf("url is required")
	}
	if a.branch == "" {
		return a, fmt.Errorf("branch is required")
	}
	if a.section == "" {
		return a, fmt.Errorf("section is required")
	}
	if a.index == "" {
		return a, fmt.Errorf("index is required")
	}
	return a, nil
}

func stringListFrom(list *starlark.List) []string {
	out := make([]string, 0, list.Len())
	iter := list.Iterate()
	defer iter.Done()
	var v starlark.Value
	for iter.Next(&v) {
		if s, ok := v.(starlark.String); ok {
			out = append(out, string(s))
		}
	}
	return out
}

func supportedArches() []string {
	out := make([]string, 0, len(archMap))
	for a := range archMap {
		out = append(out, a)
	}
	return out
}
