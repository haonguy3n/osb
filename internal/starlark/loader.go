package starlark

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type LoadOption func(*loadConfig)

type loadConfig struct {
	machine                string
	distroOverride         string
	projectFile            string
	showShadows            bool
	allowDuplicateProvides bool
	extraBuiltins          []extraBuiltin
	implicitModules        []ModuleRef
}

type extraBuiltin struct {
	name    string
	factory BuiltinFactory
}

type BuiltinFactory func(*Engine) *starlark.Builtin

func WithImplicitModules(refs []ModuleRef) LoadOption {
	return func(c *loadConfig) { c.implicitModules = refs }
}

func WithMachine(name string) LoadOption {
	return func(c *loadConfig) { c.machine = name }
}

func WithDistroOverride(distro string) LoadOption {
	return func(c *loadConfig) { c.distroOverride = distro }
}

func WithProjectFile(path string) LoadOption {
	return func(c *loadConfig) { c.projectFile = path }
}

func WithShowShadows(v bool) LoadOption {
	return func(c *loadConfig) { c.showShadows = v }
}

func WithAllowDuplicateProvides(v bool) LoadOption {
	return func(c *loadConfig) { c.allowDuplicateProvides = v }
}

func WithBuiltin(name string, factory BuiltinFactory) LoadOption {
	return func(c *loadConfig) {
		c.extraBuiltins = append(c.extraBuiltins, extraBuiltin{name: name, factory: factory})
	}
}

func LoadProject(startDir string, opts ...LoadOption) (*Project, error) {
	root, err := findProjectRoot(startDir)
	if err != nil {
		return nil, err
	}

	return LoadProjectFromRoot(root, opts...)
}

func findProjectRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}

	for {
		candidate := filepath.Join(dir, "PROJECT.star")
		if _, err := os.Stat(candidate); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("no PROJECT.star found in %s or any parent directory", startDir)
}

func LoadProjectFromRoot(root string, opts ...LoadOption) (*Project, error) {
	var cfg loadConfig
	for _, o := range opts {
		o(&cfg)
	}

	eng := NewEngine()
	eng.SetProjectRoot(root)
	eng.SetShowShadows(cfg.showShadows)
	eng.SetAllowDuplicateProvides(cfg.allowDuplicateProvides)

	if len(cfg.extraBuiltins) > 0 {
		specs := make(map[string]BuiltinFactory, len(cfg.extraBuiltins))
		for _, b := range cfg.extraBuiltins {
			specs[b.name] = b.factory
		}
		eng.SetExtraBuiltins(specs)
	}

	projFile := filepath.Join(root, "PROJECT.star")
	if cfg.projectFile != "" {
		projFile = cfg.projectFile
		if !filepath.IsAbs(projFile) {
			projFile = filepath.Join(root, projFile)
		}
	}
	if err := eng.ExecFile(projFile); err != nil {
		return nil, fmt.Errorf("evaluating %s: %w", projFile, err)
	}

	if proj := eng.Project(); proj != nil {
		if ov, err := LoadLocalOverrides(root); err == nil {
			if ov.DefaultDistroOverride != "" {
				proj.DefaultDistroOverride = ov.DefaultDistroOverride
			}
		}
		if cfg.distroOverride != "" {
			proj.DefaultDistroOverride = cfg.distroOverride
		}
	}

	if proj := eng.Project(); proj != nil && len(cfg.implicitModules) > 0 {
		proj.Modules = append(append([]ModuleRef(nil), cfg.implicitModules...), proj.Modules...)
	}

	if proj := eng.Project(); proj != nil {
		expanded, err := expandTransitiveDeps(proj.Modules, root)
		if err != nil {
			return nil, err
		}
		proj.Modules = expanded
	}

	type resolvedModule struct {
		name string
		path string
	}
	var resolvedModules []resolvedModule
	var resolvedForProject []ResolvedModule
	if proj := eng.Project(); proj != nil {
		for _, m := range proj.Modules {
			modulePath, cloneDir, ok := locateModulePath(m, root)
			rm := ResolvedModule{
				URL:       m.URL,
				Ref:       m.Ref,
				Path:      m.Path,
				Local:     m.Local,
				Available: ok,
			}
			if !ok {
				rm.Name = pathBasename(m)
				resolvedForProject = append(resolvedForProject, rm)
				continue
			}
			name := peekModuleName(modulePath)
			if name == "" {
				name = pathBasename(m)
			}
			rm.Name = name
			rm.Dir = modulePath
			rm.CloneDir = cloneDir
			resolvedForProject = append(resolvedForProject, rm)
			eng.SetModuleRoot(name, modulePath)
			resolvedModules = append(resolvedModules, resolvedModule{name: name, path: modulePath})
		}
	}

	projectIdx := len(resolvedModules) + 1

	eng.SetCurrentModule("", projectIdx)
	if err := evalDir(eng, root, "machines"); err != nil {
		return nil, err
	}
	for i, rm := range resolvedModules {
		eng.SetCurrentModule(rm.name, i+1)
		if err := evalDir(eng, rm.path, "machines"); err != nil {
			return nil, err
		}
	}

	if cfg.machine != "" {
		if _, ok := eng.Machines()[cfg.machine]; !ok {
			return nil, fmt.Errorf("machine %q not found", cfg.machine)
		}
		if proj := eng.Project(); proj != nil {
			proj.Defaults.Machine = cfg.machine
		}
	}

	arch := "x86_64"
	machine := ""
	projectVersion := ""
	var activeMachine *Machine
	if proj := eng.Project(); proj != nil {
		machine = proj.Defaults.Machine
		projectVersion = proj.Version
		if m, ok := eng.Machines()[proj.Defaults.Machine]; ok {
			arch = m.Arch
			activeMachine = m
		}
	}

	provides := starlark.NewDict(4)

	var (
		defaultDistro         string
		defaultDistroOverride string
	)
	if proj := eng.Project(); proj != nil {
		defaultDistro = proj.DefaultDistro
		defaultDistroOverride = proj.DefaultDistroOverride
	}
	ctxFields := starlark.StringDict{
		"arch":                    starlark.String(arch),
		"machine":                 starlark.String(machine),
		"project_version":         starlark.String(projectVersion),
		"provides":                provides,
		"default_distro":          starlark.String(defaultDistro),
		"default_distro_override": starlark.String(defaultDistroOverride),
	}
	if activeMachine != nil {
		ctxFields["machine_config"] = buildMachineConfigStruct(activeMachine)
	}
	eng.SetVar("ctx", starlarkstruct.FromStringDict(starlark.String("ctx"), ctxFields))

	eng.SetActiveArch(arch)
	for i, rm := range resolvedModules {
		modFile := filepath.Join(rm.path, "MODULE.star")
		if _, statErr := os.Stat(modFile); statErr != nil {
			continue
		}
		eng.SetCurrentModule(rm.name, i+1)
		if err := eng.ExecFile(modFile); err != nil {
			return nil, fmt.Errorf("evaluating %s: %w", modFile, err)
		}
	}

	if proj := eng.Project(); proj != nil {
		proj.PreferModules = mergePreferModules(eng.DefaultPreferModules(), proj.PreferModules)
	}

	if proj := eng.Project(); proj != nil && len(proj.PreferModules) > 0 {
		known := make(map[string]struct{}, len(resolvedModules)+len(eng.SyntheticModules()))
		for _, rm := range resolvedModules {
			known[rm.name] = struct{}{}
		}
		for _, sm := range eng.SyntheticModules() {
			known[sm.Name] = struct{}{}
		}
		if err := preflightPreferModules(proj.PreferModules, known); err != nil {
			return nil, err
		}
	}

	eng.SetCurrentModule("", projectIdx)
	if err := evalDir(eng, root, "containers"); err != nil {
		return nil, err
	}
	for i, rm := range resolvedModules {
		eng.SetCurrentModule(rm.name, i+1)
		if err := evalDir(eng, rm.path, "containers"); err != nil {
			return nil, err
		}
	}

	eng.SetEvalPhase("units")
	eng.SetCurrentModule("", projectIdx)
	if err := evalDir(eng, root, "units"); err != nil {
		return nil, err
	}
	for i, rm := range resolvedModules {
		eng.SetCurrentModule(rm.name, i+1)
		if err := evalDir(eng, rm.path, "units"); err != nil {
			return nil, err
		}
	}

	unitsByName := eng.Units()
	sortedUnitNames := make([]string, 0, len(unitsByName))
	for name := range unitsByName {
		sortedUnitNames = append(sortedUnitNames, name)
	}
	sort.Strings(sortedUnitNames)
	for _, uname := range sortedUnitNames {
		u := unitsByName[uname]
		for _, virt := range u.Provides {
			if virt == "" {
				continue
			}
			if existing, found, _ := provides.Get(starlark.String(virt)); found {
				existingName := string(existing.(starlark.String))
				existingUnit := eng.Units()[existingName]
				if existingUnit != nil && u.Distro != "" && existingUnit.Distro != "" && u.Distro != existingUnit.Distro {
					continue
				}
				if existingUnit == nil || u.ModuleIndex == existingUnit.ModuleIndex {
					if !eng.allowDuplicateProvides {
						return nil, fmt.Errorf("virtual package %q provided by both %q and %q",
							virt, existingName, u.Name)
					}
					continue
				}
				if u.ModuleIndex > existingUnit.ModuleIndex {
					if eng.showShadows {
						fmt.Fprintf(os.Stderr, "notice: %q from %s overrides %q via provides %q\n",
							u.Name, moduleSource(u.Module), existingName, virt)
					}
					_ = provides.SetKey(starlark.String(virt), starlark.String(u.Name))
				}
				continue
			}
			_ = provides.SetKey(starlark.String(virt), starlark.String(u.Name))
		}
	}

	if proj := eng.Project(); proj != nil {
		proj.Provides = map[string]string{}
		for _, item := range provides.Items() {
			k, kok := item[0].(starlark.String)
			v, vok := item[1].(starlark.String)
			if kok && vok {
				proj.Provides[string(k)] = string(v)
			}
		}
	}

	eng.SetEvalPhase("images")
	eng.SetCurrentModule("", projectIdx)
	if err := evalDir(eng, root, "images"); err != nil {
		return nil, err
	}
	for i, rm := range resolvedModules {
		eng.SetCurrentModule(rm.name, i+1)
		if err := evalDir(eng, rm.path, "images"); err != nil {
			return nil, err
		}
	}

	proj := eng.Project()
	if proj == nil {
		return nil, fmt.Errorf("PROJECT.star did not call project()")
	}

	proj.Machines = eng.Machines()
	proj.UnitsByModule = eng.UnitsByModule()
	proj.ResolvedModules = resolvedForProject
	proj.Diagnostics.Shadows = eng.Shadows()

	synths := eng.SyntheticModules()
	if len(synths) > 0 {
		for i, sm := range synths {
			sm.Priority = -i
		}
		proj.SyntheticModules = synths
	}

	proj.Provides = map[string]string{}
	for _, item := range provides.Items() {
		k, kok := item[0].(starlark.String)
		v, vok := item[1].(starlark.String)
		if kok && vok {
			proj.Provides[string(k)] = string(v)
		}
	}

	virtToUnits := map[string][]string{}
	for _, u := range proj.AllUnits() {
		for _, virt := range u.Provides {
			if virt == "" {
				continue
			}
			virtToUnits[virt] = append(virtToUnits[virt], u.Name)
		}
	}
	var virts []string
	for v := range virtToUnits {
		if len(virtToUnits[v]) > 1 {
			virts = append(virts, v)
		}
	}
	sort.Strings(virts)
	for _, v := range virts {
		claimants := virtToUnits[v]
		sort.Strings(claimants)
		active := proj.Provides[v]
		var others []string
		for _, c := range claimants {
			if c != active {
				others = append(others, c)
			}
		}
		proj.Diagnostics.DuplicateProvides = append(proj.Diagnostics.DuplicateProvides, ProvidesEvent{
			Virtual: v,
			Active:  active,
			Others:  others,
		})
	}

	for name, u := range proj.AllUnits() {
		if len(u.Tasks) == 0 {
			continue
		}
		if u.Class == "container" {
			continue
		}
		if u.Container == "" {
			return nil, fmt.Errorf("unit %q has tasks but no container - set container in the unit or class", name)
		}
		if u.ContainerArch == "" {
			return nil, fmt.Errorf("unit %q has tasks but no container_arch - set container_arch in the unit or class", name)
		}
	}

	distroSet := map[string]struct{}{}
	if proj := eng.Project(); proj != nil {
		if d, err := proj.EffectiveDistro(); err == nil {
			distroSet[d] = struct{}{}
		}
	}
	for _, byName := range eng.UnitsByModule() {
		for _, u := range byName {
			if u.Class == "image" && u.Distro != "" {
				distroSet[u.Distro] = struct{}{}
			}
		}
	}
	for {
		added := 0
		for d := range distroSet {
			for name := range eng.Units() {
				unit := eng.findVisibleByName(name, d)
				if unit == nil {
					continue
				}
				edges := append(append([]string{}, unit.DepsForDistro(d)...), unit.RuntimeDepsForDistro(d)...)
				for _, dep := range edges {
					resolved := eng.resolveProvidesForDistro(dep, d)
					if eng.findVisibleByName(resolved, d) != nil {
						continue
					}
					u, err := eng.lookupOrMaterialize(resolved, d)
					if err != nil {
						return nil, fmt.Errorf("materializing dep %q of unit %q (distro %q): %w", dep, name, d, err)
					}
					if u != nil {
						added++
					}
				}
			}
		}
		if added == 0 {
			break
		}
	}

	if err := validatePreferModules(proj); err != nil {
		return nil, err
	}

	proj.UnitsByModule = eng.UnitsByModule()

	proj.DistroViews = buildDistroViews(proj)

	return proj, nil
}

func buildDistroViews(proj *Project) map[string]map[string]*Unit {
	if proj == nil {
		return nil
	}
	distros := map[string]struct{}{}
	if proj.DefaultDistro != "" {
		distros[proj.DefaultDistro] = struct{}{}
	}
	if proj.DefaultDistroOverride != "" {
		distros[proj.DefaultDistroOverride] = struct{}{}
	}
	for d := range proj.PreferModules {
		distros[d] = struct{}{}
	}
	allNames := map[string]struct{}{}
	for _, byName := range proj.UnitsByModule {
		for name, u := range byName {
			allNames[name] = struct{}{}
			if u.Distro != "" {
				distros[u.Distro] = struct{}{}
			}
		}
	}

	views := make(map[string]map[string]*Unit, len(distros))
	for distro := range distros {
		view := make(map[string]*Unit, len(allNames))
		for name := range allNames {
			if u := resolveForDistro(proj, distro, name); u != nil {
				view[name] = u
			}
		}
		views[distro] = view
	}
	return views
}

func resolveForDistro(proj *Project, distro, name string) *Unit {
	if pins, ok := proj.PreferModules[distro]; ok {
		if pinned, ok := pins[name]; ok && pinned != "" {
			if byName, ok := proj.UnitsByModule[pinned]; ok {
				if u, ok := byName[name]; ok && unitVisibleToDistro(u, distro) {
					return u
				}
			}
		}
	}
	var best *Unit
	for _, byName := range proj.UnitsByModule {
		u, ok := byName[name]
		if !ok {
			continue
		}
		if !unitVisibleToDistro(u, distro) {
			continue
		}
		if best == nil || u.ModuleIndex > best.ModuleIndex {
			best = u
		}
	}
	return best
}

func unitVisibleToDistro(u *Unit, distro string) bool {
	if u == nil {
		return false
	}
	if distro == "" {
		return true
	}
	return u.Distro == "" || u.Distro == distro
}

func mergePreferModules(defaults, project map[string]map[string]string) map[string]map[string]string {
	if len(defaults) == 0 {
		return project
	}
	merged := make(map[string]map[string]string, len(defaults)+len(project))
	for distro, pins := range defaults {
		m := make(map[string]string, len(pins))
		for unit, mod := range pins {
			m[unit] = mod
		}
		merged[distro] = m
	}
	for distro, pins := range project {
		if merged[distro] == nil {
			merged[distro] = make(map[string]string, len(pins))
		}
		for unit, mod := range pins {
			merged[distro][unit] = mod
		}
	}
	return merged
}

func validatePreferModules(proj *Project) error {
	if proj == nil || len(proj.PreferModules) == 0 {
		return nil
	}
	known := make(map[string]struct{}, len(proj.ResolvedModules)+len(proj.SyntheticModules))
	for _, rm := range proj.ResolvedModules {
		if rm.Name != "" {
			known[rm.Name] = struct{}{}
		}
	}
	for _, sm := range proj.SyntheticModules {
		if sm.Name != "" {
			known[sm.Name] = struct{}{}
		}
	}
	return preflightPreferModules(proj.PreferModules, known)
}

func preflightPreferModules(prefer map[string]map[string]string, known map[string]struct{}) error {
	for distro, pins := range prefer {
		for unit, modName := range pins {
			if modName == "" {
				continue
			}
			if _, ok := known[modName]; ok {
				continue
			}
			suggestions := suggestModuleNames(modName, known)
			hint := ""
			switch len(suggestions) {
			case 0:
			case 1:
				hint = fmt.Sprintf(" Did you mean %q?", suggestions[0])
			default:
				quoted := make([]string, len(suggestions))
				for i, s := range suggestions {
					quoted[i] = fmt.Sprintf("%q", s)
				}
				hint = fmt.Sprintf(" Did you mean one of: %s?", strings.Join(quoted, ", "))
			}
			return fmt.Errorf(
				`prefer_modules[%q] entry %q: %q - module %q not found.%s See docs/naming-and-resolution.md "Feeds as synthetic modules" for the alpine → alpine.main/alpine.community migration.`,
				distro, unit, modName, modName, hint)
		}
	}
	return nil
}

func suggestModuleNames(target string, known map[string]struct{}) []string {
	var prefixed, contained []string
	for name := range known {
		switch {
		case name == target:
			continue
		case strings.HasPrefix(name, target+"."):
			prefixed = append(prefixed, name)
		case strings.Contains(name, target):
			contained = append(contained, name)
		}
	}
	sort.Strings(prefixed)
	sort.Strings(contained)
	out := append(prefixed, contained...)
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func toStarlarkStringList(ss []string) *starlark.List {
	vals := make([]starlark.Value, len(ss))
	for i, s := range ss {
		vals[i] = starlark.String(s)
	}
	return starlark.NewList(vals)
}

func pathBasename(m ModuleRef) string {
	if m.Path != "" {
		return filepath.Base(m.Path)
	}
	return filepath.Base(strings.TrimSuffix(m.URL, ".git"))
}

func locateModulePath(m ModuleRef, projectRoot string) (modulePath, cloneDir string, ok bool) {
	base := pathBasename(m)
	if m.Local != "" {
		cloneDir = m.Local
		if !filepath.IsAbs(cloneDir) {
			cloneDir = filepath.Join(projectRoot, cloneDir)
		}
		modulePath = cloneDir
		if m.Path != "" {
			modulePath = filepath.Join(cloneDir, m.Path)
		}
		return modulePath, cloneDir, true
	}
	cacheDir := os.Getenv("OSB_CACHE")
	if cacheDir == "" {
		cacheDir = "cache"
	}
	cloneDir = filepath.Join(cacheDir, "modules", base)
	modulePath = cloneDir
	if m.Path != "" {
		modulePath = filepath.Join(cloneDir, m.Path)
	}
	if _, err := os.Stat(modulePath); err != nil {
		return "", "", false
	}
	return modulePath, cloneDir, true
}

func peekModuleName(modulePath string) string {
	info := peekModuleInfo(modulePath)
	if info == nil {
		return ""
	}
	return info.Name
}

func peekModuleInfo(modulePath string) *ModuleInfo {
	file := filepath.Join(modulePath, "MODULE.star")
	src, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	info := &ModuleInfo{}
	moduleInfo := starlark.NewBuiltin("module_info",
		func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			for _, kv := range kwargs {
				key, ok := kv[0].(starlark.String)
				if !ok {
					continue
				}
				switch string(key) {
				case "name":
					if v, ok := kv[1].(starlark.String); ok {
						info.Name = string(v)
					}
				case "description":
					if v, ok := kv[1].(starlark.String); ok {
						info.Description = string(v)
					}
				case "deps":
					info.Deps = parsePeekDeps(kv[1])
				}
			}
			return starlark.None, nil
		})
	moduleBuiltin := starlark.NewBuiltin("module",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			ref := ModuleRef{}
			if len(args) >= 1 {
				if v, ok := args[0].(starlark.String); ok {
					ref.URL = string(v)
				}
			}
			for _, kv := range kwargs {
				key, ok := kv[0].(starlark.String)
				if !ok {
					continue
				}
				switch string(key) {
				case "url":
					if v, ok := kv[1].(starlark.String); ok {
						ref.URL = string(v)
					}
				case "ref":
					if v, ok := kv[1].(starlark.String); ok {
						ref.Ref = string(v)
					}
				case "path":
					if v, ok := kv[1].(starlark.String); ok {
						ref.Path = string(v)
					}
				case "local":
					if v, ok := kv[1].(starlark.String); ok {
						ref.Local = string(v)
					}
				}
			}
			return moduleRefValue{ref: ref}, nil
		})
	noop := starlark.NewBuiltin("noop",
		func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			return starlark.None, nil
		})
	thread := &starlark.Thread{Name: file}
	_, _ = starlark.ExecFileOptions(fileOpts, thread, file, src, starlark.StringDict{
		"module_info": moduleInfo,
		"module":      moduleBuiltin,
		"alpine_feed": noop,
		"apt_feed":    noop,
	})
	return info
}

type moduleRefValue struct{ ref ModuleRef }

func (moduleRefValue) String() string        { return "module_ref" }
func (moduleRefValue) Type() string          { return "module_ref" }
func (moduleRefValue) Freeze()               {}
func (moduleRefValue) Truth() starlark.Bool  { return starlark.True }
func (moduleRefValue) Hash() (uint32, error) { return 0, fmt.Errorf("module_ref is not hashable") }

func parsePeekDeps(v starlark.Value) []ModuleRef {
	list, ok := v.(*starlark.List)
	if !ok {
		return nil
	}
	out := make([]ModuleRef, 0, list.Len())
	iter := list.Iterate()
	defer iter.Done()
	var item starlark.Value
	for iter.Next(&item) {
		if mr, ok := item.(moduleRefValue); ok {
			out = append(out, mr.ref)
		}
	}
	return out
}

func buildMachineConfigStruct(m *Machine) *starlarkstruct.Struct {
	kernel := starlark.NewDict(len(m.Kernel))
	for k, v := range m.Kernel {
		_ = kernel.SetKey(starlark.String(k), starlark.String(v))
	}
	distroPackages := starlark.NewDict(len(m.DistroPackages))
	for k, v := range m.DistroPackages {
		_ = distroPackages.SetKey(starlark.String(k), toStarlarkStringList(v))
	}
	return starlarkstruct.FromStringDict(starlark.String("machine_config"), starlark.StringDict{
		"name":            starlark.String(m.Name),
		"arch":            starlark.String(m.Arch),
		"firmware":        starlark.String(m.Firmware),
		"bootloader":      starlark.String(m.Bootloader),
		"console":         starlark.String(m.Console),
		"cmdline":         starlark.String(m.Cmdline),
		"kernel":          kernel,
		"packages":        toStarlarkStringList(m.Packages),
		"distro_packages": distroPackages,
	})
}

func evalDir(eng *Engine, root, subdir string) error {
	base := filepath.Join(root, subdir)
	return filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".star") {
			return nil
		}
		if err := eng.ExecFile(path); err != nil {
			return fmt.Errorf("evaluating %s: %w", path, err)
		}
		return nil
	})
}
