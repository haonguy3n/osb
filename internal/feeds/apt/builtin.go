package apt

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"go.starlark.net/starlark"

	"github.com/anhhao17/osb/internal/dpkg"
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
	"x86_64": "amd64",
	"arm64":  "arm64",
}

func Builtin(eng *osbstar.Engine) *starlark.Builtin {
	return starlark.NewBuiltin("apt_feed", makeAptFeed(eng))
}

func makeAptFeed(eng *osbstar.Engine) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		args, err := parseKwargs(kwargs)
		if err != nil {
			return nil, fmt.Errorf("apt_feed: %w", err)
		}

		parent := eng.CurrentModule()
		if parent == "" {
			return nil, fmt.Errorf("apt_feed: must be called from a module's MODULE.star (not the project root)")
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
			return nil, fmt.Errorf("apt_feed %q: %w", composedName, err)
		}
		return starlark.None, nil
	}
}

func buildSyntheticModule(eng *osbstar.Engine, composedName, parent, indexRoot string, args aptFeedArgs) *osbstar.SyntheticModule {
	s := &archState{
		indexRoot: indexRoot,
		eng:       eng,
		byArch:    make(map[string]*archCache),
		feedArgs:  args,
	}
	registerFeedState(eng, s)

	return &osbstar.SyntheticModule{
		Name:   composedName,
		Parent: parent,
		Suite:  args.suite,
		Distro: args.distro,
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
	feedArgs  aptFeedArgs
}

type archCache struct {
	entries  []dpkg.Entry
	provides *dpkg.ProvidesTable
	byName   map[string]*dpkg.Entry
}

func (s *archState) cacheFor(arch string) (*archCache, error) {
	if c, ok := s.byArch[arch]; ok {
		return c, nil
	}
	debArch, ok := archMap[arch]
	if !ok {
		return nil, fmt.Errorf("apt_feed: unsupported arch %q (supported: %s)",
			arch, strings.Join(supportedArches(), ", "))
	}
	indexPath := filepath.Join(s.indexRoot, debArch, "Packages")
	entries, err := dpkg.ParseIndexFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("apt_feed: load %s: %w", indexPath, err)
	}
	table := dpkg.BuildProvidesTable(entries)
	byName := make(map[string]*dpkg.Entry, len(entries))
	for i := range entries {
		byName[entries[i].Package] = &entries[i]
	}
	c := &archCache{entries: entries, provides: table, byName: byName}
	s.byArch[arch] = c
	return c, nil
}

func (s *archState) lookup(moduleName, name string) (*osbstar.Unit, error) {
	arch := s.eng.ActiveArch()
	if arch == "" {
		return nil, fmt.Errorf("apt_feed: no active arch (machine not loaded?)")
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
	u, err := dpkg.MaterializeUnit(*entry, providers, moduleName, s.feedArgs.distro)
	if err != nil {
		return nil, err
	}
	s.populateBuildFields(u, entry, arch)
	return u, nil
}

func (s *archState) populateBuildFields(u *osbstar.Unit, entry *dpkg.Entry, arch string) {
	asset := filepath.Base(entry.Filename)
	if asset == "." || asset == "" {
		asset = fmt.Sprintf("%s_%s_%s.deb", entry.Package, entry.Version, entry.Architecture)
	}
	u.Source = fmt.Sprintf("%s/%s",
		strings.TrimSuffix(s.feedArgs.baseURLFor(arch), "/"),
		entry.Filename,
	)
	u.SHA256 = entry.SHA256
	u.PassthroughAPK = ""
	u.PassthroughDeb = asset
	u.Container = "toolchain"
	u.ContainerArch = "target"
	u.Sandbox = false
	u.Tasks = []osbstar.Task{
		{
			Name: "install",
			Steps: []osbstar.Step{
				{Command: "mkdir -p $DESTDIR"},
				{Command: "dpkg-deb --fsys-tarfile ./" + asset + " | tar -xpf - -C $DESTDIR"},
			},
		},
	}
}

type multiFeedProviders struct {
	primary  *dpkg.ProvidesTable
	siblings []*dpkg.ProvidesTable
}

func newMultiFeedProviders(eng *osbstar.Engine, arch string, primary *dpkg.ProvidesTable) multiFeedProviders {
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
		return e.Package, true
	}
	for _, t := range m.siblings {
		if e := t.Lookup(token); e != nil {
			return e.Package, true
		}
	}
	return "", false
}

func (s *archState) provides(arch string) *dpkg.ProvidesTable {
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
		out = append(out, c.entries[i].Package)
	}
	return out
}

type aptFeedArgs struct {
	name      string
	distro    string
	url       string
	archURLs  map[string]string
	suite     string
	component string
	arches    []string
	index     string
	keyring   string
}

func (a aptFeedArgs) baseURLFor(arch string) string {
	if u, ok := a.archURLs[arch]; ok && u != "" {
		return u
	}
	return a.url
}

func parseKwargs(kwargs []starlark.Tuple) (aptFeedArgs, error) {
	var a aptFeedArgs
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
		case "distro":
			if v, ok := kv[1].(starlark.String); ok {
				a.distro = string(v)
			}
		case "url":
			if v, ok := kv[1].(starlark.String); ok {
				a.url = string(v)
			}
		case "arch_urls":
			if d, ok := kv[1].(*starlark.Dict); ok {
				a.archURLs = stringDictFrom(d)
			}
		case "suite":
			if v, ok := kv[1].(starlark.String); ok {
				a.suite = string(v)
			}
		case "component":
			if v, ok := kv[1].(starlark.String); ok {
				a.component = string(v)
			}
		case "arches":
			if list, ok := kv[1].(*starlark.List); ok {
				a.arches = stringListFrom(list)
			}
		case "index":
			if v, ok := kv[1].(starlark.String); ok {
				a.index = string(v)
			}
		case "keyring":
			if v, ok := kv[1].(starlark.String); ok {
				a.keyring = string(v)
			}
		}
	}
	if a.name == "" {
		return a, fmt.Errorf("name is required")
	}
	if a.distro == "" {
		return a, fmt.Errorf("distro is required (e.g. \"debian\" or \"ubuntu\")")
	}
	if a.url == "" {
		return a, fmt.Errorf("url is required")
	}
	if a.suite == "" {
		return a, fmt.Errorf("suite is required")
	}
	if a.component == "" {
		return a, fmt.Errorf("component is required")
	}
	if a.index == "" {
		return a, fmt.Errorf("index is required")
	}
	if len(a.arches) == 0 {
		return a, fmt.Errorf("arches is required")
	}
	return a, nil
}

func stringDictFrom(d *starlark.Dict) map[string]string {
	out := make(map[string]string, d.Len())
	for _, item := range d.Items() {
		k, kok := item[0].(starlark.String)
		v, vok := item[1].(starlark.String)
		if kok && vok {
			out[string(k)] = string(v)
		}
	}
	return out
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
