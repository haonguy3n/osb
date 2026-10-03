package starlark

import (
	"fmt"
	"os"
	"path/filepath"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// builtins returns the predeclared names available in all .star files.
func (e *Engine) builtins() starlark.StringDict {
	d := starlark.StringDict{
		"project":          starlark.NewBuiltin("project", e.fnProject),
		"defaults":         starlark.NewBuiltin("defaults", fnDefaults),
		"cache":            starlark.NewBuiltin("cache", fnCache),
		"s3_cache":         starlark.NewBuiltin("s3_cache", fnS3Cache),
		"sources":          starlark.NewBuiltin("sources", fnSources),
		"module":           starlark.NewBuiltin("module", fnModule),
		"module_info":      starlark.NewBuiltin("module_info", e.fnModuleInfo),
		"machine":          starlark.NewBuiltin("machine", e.fnMachine),
		"qemu_config":      starlark.NewBuiltin("qemu_config", fnQEMUConfig),
		"unit":             starlark.NewBuiltin("unit", e.fnUnit),
		"image":            starlark.NewBuiltin("image", e.fnImage),
		"task":             starlark.NewBuiltin("task", fnTask),
		"command":          starlark.NewBuiltin("command", e.fnCommand),
		"arg":              starlark.NewBuiltin("arg", fnArg),
		"run":              buildTimeBuiltin("run"),
		"install_uki":      buildTimeBuiltin("install_uki"),
		"install_file":     starlark.NewBuiltin("install_file", fnInstallFile),
		"install_template": starlark.NewBuiltin("install_template", fnInstallTemplate),
		"resolve_closure":  starlark.NewBuiltin("resolve_closure", e.fnResolveClosure),
		"True":             starlark.True,
		"False":            starlark.False,
	}

	// Merge engine variables (e.g., ARCH set after machine loading).
	for k, v := range e.vars {
		d[k] = v
	}

	// Merge extra builtins registered via WithBuiltin LoadOption.
	// Materialized once (SetExtraBuiltins) so each factory runs against
	// the live Engine without re-allocating per ExecFile call.
	for k, v := range e.extraBuiltins {
		d[k] = v
	}

	return d
}

func buildTimeBuiltin(name string) *starlark.Builtin {
	return starlark.NewBuiltin(name, func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if fn, ok := thread.Local("osb." + name).(starlark.Callable); ok && thread.Local("osb.sandbox") != nil {
			return starlark.Call(thread, fn, args, kwargs)
		}
		return nil, fmt.Errorf("%s() can only be called at build time (inside a task function)", name)
	})
}

// --- Helper: extract keyword args ---

func kwString(kwargs []starlark.Tuple, key string) string {
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == key {
			if s, ok := kv[1].(starlark.String); ok {
				return string(s)
			}
		}
	}
	return ""
}

// ParseTaskList converts a Starlark list of task structs into Go Task values.
func ParseTaskList(list *starlark.List) []Task {
	var tasks []Task
	iter := list.Iterate()
	defer iter.Done()
	var v starlark.Value
	for iter.Next(&v) {
		s, ok := v.(*starlarkstruct.Struct)
		if !ok {
			continue
		}
		t := Task{
			Name:      structString(s, "name"),
			Container: structString(s, "container"),
		}
		if rv, err := s.Attr("run"); err == nil {
			if cmd, ok := rv.(starlark.String); ok {
				t.Steps = []Step{{Command: string(cmd)}}
			}
		}
		if rv, err := s.Attr("fn"); err == nil {
			if fn, ok := rv.(starlark.Callable); ok {
				t.Steps = []Step{{Fn: fn}}
			}
		}
		if rv, err := s.Attr("steps"); err == nil {
			if list, ok := rv.(*starlark.List); ok {
				si := list.Iterate()
				var sv starlark.Value
				for si.Next(&sv) {
					switch val := sv.(type) {
					case starlark.String:
						t.Steps = append(t.Steps, Step{Command: string(val)})
					case *InstallStepValue:
						t.Steps = append(t.Steps, Step{Install: &InstallStep{
							Kind:    val.Kind,
							Src:     val.Src,
							Dest:    val.Dest,
							Mode:    val.Mode,
							BaseDir: val.BaseDir,
						}})
					case starlark.Callable:
						t.Steps = append(t.Steps, Step{Fn: val})
					}
				}
				si.Done()
			}
		}
		tasks = append(tasks, t)
	}
	return tasks
}

func kwBool(kwargs []starlark.Tuple, key string) bool {
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == key {
			if b, ok := kv[1].(starlark.Bool); ok {
				return bool(b)
			}
		}
	}
	return false
}

func kwInt(kwargs []starlark.Tuple, key string) int {
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == key {
			if n, ok := kv[1].(starlark.Int); ok {
				v, _ := n.Int64()
				return int(v)
			}
		}
	}
	return 0
}

func kwStringList(kwargs []starlark.Tuple, key string) []string {
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == key {
			if list, ok := kv[1].(*starlark.List); ok {
				var result []string
				iter := list.Iterate()
				defer iter.Done()
				var v starlark.Value
				for iter.Next(&v) {
					if s, ok := v.(starlark.String); ok {
						result = append(result, string(s))
					}
				}
				return result
			}
		}
	}
	return nil
}

// kwStringListMap parses a kwarg shaped like
// `{"alpine": ["a", "b"], "debian": ["c"]}` into map[string][]string.
// Used for distro_deps / distro_runtime_deps where each distro key
// names additional deps that apply only to that distro's closure.
func kwStringListMap(kwargs []starlark.Tuple, key string) map[string][]string {
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) != key {
			continue
		}
		d, ok := kv[1].(*starlark.Dict)
		if !ok {
			return nil
		}
		m := make(map[string][]string, d.Len())
		for _, item := range d.Items() {
			k, ok := item[0].(starlark.String)
			if !ok {
				continue
			}
			list, ok := item[1].(*starlark.List)
			if !ok {
				continue
			}
			var values []string
			iter := list.Iterate()
			var v starlark.Value
			for iter.Next(&v) {
				if s, ok := v.(starlark.String); ok {
					values = append(values, string(s))
				}
			}
			iter.Done()
			m[string(k)] = values
		}
		return m
	}
	return nil
}

func kwStringMap(kwargs []starlark.Tuple, key string) map[string]string {
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == key {
			if d, ok := kv[1].(*starlark.Dict); ok {
				m := make(map[string]string, d.Len())
				for _, item := range d.Items() {
					if k, ok := item[0].(starlark.String); ok {
						if v, ok := item[1].(starlark.String); ok {
							m[string(k)] = string(v)
						}
					}
				}
				return m
			}
		}
	}
	return nil
}

// reservedUnitKwargs lists the kwargs that unit() and image() map to typed
// fields on the Unit struct. Kwargs not in this set are captured into
// Unit.Extra for template context rendering.
//
// When a new typed field is added to the Unit struct, add its kwarg name here
// too so it isn't double-captured into Extra.
var reservedUnitKwargs = map[string]bool{
	"name": true, "version": true, "release": true, "scope": true,
	"description": true, "license": true, "distro": true,
	"source": true, "sha256": true,
	"apk_checksum":    true,
	"passthrough_apk": true,
	"tag":             true, "branch": true, "patches": true, "deps": true,
	"runtime_deps":        true,
	"distro_deps":         true,
	"distro_runtime_deps": true,
	"container":           true, "container_arch": true,
	"sandbox": true, "shell": true, "tasks": true, "provides": true,
	"replaces": true,
	"services": true, "conffiles": true, "environment": true, "owners": true,
	"cache_dirs": true, "packages": true, "boot": true, "unit_class": true,
}

// starlarkToGo converts a Starlark value into a Go value suitable for JSON
// serialization and Go template rendering. Returns an error for unsupported
// types so unit definitions fail loudly instead of silently dropping data.
func starlarkToGo(v starlark.Value) (any, error) {
	switch x := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(x), nil
	case starlark.String:
		return string(x), nil
	case starlark.Int:
		n, ok := x.Int64()
		if !ok {
			return nil, fmt.Errorf("int value out of int64 range")
		}
		return n, nil
	case starlark.Float:
		return float64(x), nil
	case *starlark.List:
		out := make([]any, 0, x.Len())
		it := x.Iterate()
		defer it.Done()
		var item starlark.Value
		for it.Next(&item) {
			g, err := starlarkToGo(item)
			if err != nil {
				return nil, err
			}
			out = append(out, g)
		}
		return out, nil
	case *starlark.Dict:
		out := make(map[string]any, x.Len())
		for _, kv := range x.Items() {
			ks, ok := kv[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("dict key must be string, got %s", kv[0].Type())
			}
			g, err := starlarkToGo(kv[1])
			if err != nil {
				return nil, err
			}
			out[string(ks)] = g
		}
		return out, nil
	case starlark.Tuple:
		return starlarkToGo(starlark.NewList(append([]starlark.Value{}, x...)))
	case *starlarkstruct.Struct:
		d := starlark.StringDict{}
		x.ToStringDict(d)
		out := make(map[string]any, len(d))
		for k, val := range d {
			g, err := starlarkToGo(val)
			if err != nil {
				return nil, err
			}
			out[k] = g
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported type %s", v.Type())
	}
}

func kwStruct(kwargs []starlark.Tuple, key string) *starlarkstruct.Struct {
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == key {
			if s, ok := kv[1].(*starlarkstruct.Struct); ok {
				return s
			}
		}
	}
	return nil
}

func structString(s *starlarkstruct.Struct, field string) string {
	if s == nil {
		return ""
	}
	v, err := s.Attr(field)
	if err != nil {
		return ""
	}
	if str, ok := v.(starlark.String); ok {
		return string(str)
	}
	return ""
}

func structBool(s *starlarkstruct.Struct, field string) bool {
	if s == nil {
		return false
	}
	v, err := s.Attr(field)
	if err != nil {
		return false
	}
	if b, ok := v.(starlark.Bool); ok {
		return bool(b)
	}
	return false
}

func structStringMap(s *starlarkstruct.Struct, field string) map[string]string {
	if s == nil {
		return nil
	}
	v, err := s.Attr(field)
	if err != nil {
		return nil
	}
	dict, ok := v.(*starlark.Dict)
	if !ok {
		return nil
	}
	result := make(map[string]string, dict.Len())
	for _, item := range dict.Items() {
		k, kok := item[0].(starlark.String)
		val, vok := item[1].(starlark.String)
		if kok && vok {
			result[string(k)] = string(val)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func structStringList(s *starlarkstruct.Struct, field string) []string {
	if s == nil {
		return nil
	}
	v, err := s.Attr(field)
	if err != nil {
		return nil
	}
	if list, ok := v.(*starlark.List); ok {
		var result []string
		iter := list.Iterate()
		defer iter.Done()
		var item starlark.Value
		for iter.Next(&item) {
			if str, ok := item.(starlark.String); ok {
				result = append(result, string(str))
			}
		}
		return result
	}
	return nil
}

// --- Built-in functions that return structs (data constructors) ---

func makeStruct(name string, kwargs []starlark.Tuple) *starlarkstruct.Struct {
	d := make(starlark.StringDict, len(kwargs))
	for _, kv := range kwargs {
		d[string(kv[0].(starlark.String))] = kv[1]
	}
	return starlarkstruct.FromStringDict(starlark.String(name), d)
}

func fnDefaults(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return makeStruct("defaults", kwargs), nil
}

func fnCache(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return makeStruct("cache", kwargs), nil
}

func fnS3Cache(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return makeStruct("s3_cache", kwargs), nil
}

func fnSources(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return makeStruct("sources", kwargs), nil
}

func fnModule(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("module() requires a URL argument")
	}
	url, ok := args[0].(starlark.String)
	if !ok {
		return nil, fmt.Errorf("module() URL must be a string")
	}
	d := starlark.StringDict{"url": url}
	for _, kv := range kwargs {
		d[string(kv[0].(starlark.String))] = kv[1]
	}
	return starlarkstruct.FromStringDict(starlark.String("module"), d), nil
}

func fnQEMUConfig(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return makeStruct("qemu_config", kwargs), nil
}

// --- Built-in functions that register module info ---

func (e *Engine) fnModuleInfo(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	name := kwString(kwargs, "name")
	if name == "" {
		return nil, fmt.Errorf("module_info() requires name")
	}

	info := &ModuleInfo{
		Name:        name,
		Description: kwString(kwargs, "description"),
	}

	// Parse deps list of module() structs
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == "deps" {
			if list, ok := kv[1].(*starlark.List); ok {
				iter := list.Iterate()
				defer iter.Done()
				var v starlark.Value
				for iter.Next(&v) {
					if s, ok := v.(*starlarkstruct.Struct); ok {
						info.Deps = append(info.Deps, ModuleRef{
							URL: structString(s, "url"),
							Ref: structString(s, "ref"),
						})
					}
				}
			}
		}
	}

	// Module-declared default pins; accumulate in evaluation order
	// (later modules win per key), project pins merge on top in the
	// loader. See docs/naming-and-resolution.md "prefer_modules".
	prefer, err := parsePreferModules(kwargs, "module_info")
	if err != nil {
		return nil, err
	}
	for distro, pins := range prefer {
		if e.defaultPreferModules == nil {
			e.defaultPreferModules = make(map[string]map[string]string)
		}
		if e.defaultPreferModules[distro] == nil {
			e.defaultPreferModules[distro] = make(map[string]string, len(pins))
		}
		for unit, mod := range pins {
			e.defaultPreferModules[distro][unit] = mod
		}
	}

	e.moduleInfo = info
	return starlark.None, nil
}

// --- Built-in functions that register targets (side-effecting) ---

func (e *Engine) fnProject(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.project != nil {
		return nil, fmt.Errorf("project() called more than once")
	}

	defs := kwStruct(kwargs, "defaults")
	cacheS := kwStruct(kwargs, "cache")

	p := &Project{
		Name:    kwString(kwargs, "name"),
		Version: kwString(kwargs, "version"),
		Defaults: Defaults{
			Machine: structString(defs, "machine"),
			Image:   structString(defs, "image"),
		},
		Cache: CacheConfig{
			Path: structString(cacheS, "path"),
		},
		SigningKey:    kwString(kwargs, "signing_key"),
		DefaultDistro: structString(defs, "distro"),
	}

	// Parse modules list
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == "modules" {
			if list, ok := kv[1].(*starlark.List); ok {
				iter := list.Iterate()
				defer iter.Done()
				var v starlark.Value
				for iter.Next(&v) {
					if s, ok := v.(*starlarkstruct.Struct); ok {
						p.Modules = append(p.Modules, ModuleRef{
							URL:   structString(s, "url"),
							Ref:   structString(s, "ref"),
							Path:  structString(s, "path"),
							Local: structString(s, "local"),
						})
					}
				}
			}
		}
	}

	prefer, err := parsePreferModules(kwargs, "project")
	if err != nil {
		return nil, err
	}
	p.PreferModules = prefer

	e.project = p
	return starlark.None, nil
}

// parsePreferModules parses a prefer_modules kwarg - a per-distro dict
// {"<distro>": {"<unit>": "<module>"}} (docs/naming-and-resolution.md).
// Returns nil when absent; context names the calling builtin for errors.
func parsePreferModules(kwargs []starlark.Tuple, context string) (map[string]map[string]string, error) {
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) != "prefer_modules" {
			continue
		}
		d, ok := kv[1].(*starlark.Dict)
		if !ok {
			return nil, fmt.Errorf("%s: prefer_modules must be a dict", context)
		}
		prefer := make(map[string]map[string]string, d.Len())
		for _, pair := range d.Items() {
			distroKey, dok := pair.Index(0).(starlark.String)
			inner, iok := pair.Index(1).(*starlark.Dict)
			if !dok || !iok {
				return nil, fmt.Errorf("%s: prefer_modules outer keys must be distro strings and values must be dicts of {unit: module}", context)
			}
			distroName := string(distroKey)
			pins := make(map[string]string, inner.Len())
			for _, ipair := range inner.Items() {
				k, kok := ipair.Index(0).(starlark.String)
				v, vok := ipair.Index(1).(starlark.String)
				if !kok || !vok {
					return nil, fmt.Errorf("%s: prefer_modules[%q] keys and values must be strings", context, distroName)
				}
				pins[string(k)] = string(v)
			}
			prefer[distroName] = pins
		}
		return prefer, nil
	}
	return nil, nil
}

func (e *Engine) fnMachine(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	name := kwString(kwargs, "name")
	arch := kwString(kwargs, "arch")
	if name == "" {
		return nil, fmt.Errorf("machine() requires name")
	}
	if !validArchitectures[arch] {
		return nil, fmt.Errorf("machine %q: invalid arch %q (valid: arm64, x86_64)", name, arch)
	}
	m := &Machine{
		Name:           name,
		Arch:           arch,
		Description:    kwString(kwargs, "description"),
		Firmware:       kwString(kwargs, "firmware"),
		Bootloader:     kwString(kwargs, "bootloader"),
		Console:        kwString(kwargs, "console"),
		Cmdline:        kwString(kwargs, "cmdline"),
		Packages:       kwStringList(kwargs, "packages"),
		DistroPackages: kwStringListMap(kwargs, "distro_packages"),
	}
	if m.Firmware == "" {
		m.Firmware = FirmwareUEFI
	}
	if m.Firmware != FirmwareUEFI && m.Firmware != FirmwareBIOS {
		return nil, fmt.Errorf("machine %q: invalid firmware %q (valid: uefi, bios)", name, m.Firmware)
	}
	if m.Firmware == FirmwareBIOS && arch != "x86_64" {
		return nil, fmt.Errorf("machine %q: bios firmware is x86_64-only", name)
	}
	for _, kv := range kwargs {
		switch string(kv[0].(starlark.String)) {
		case "kernel":
			switch v := kv[1].(type) {
			case starlark.String:
				m.Kernel = map[string]string{"": string(v)}
			default:
				m.Kernel = kwStringMap(kwargs, "kernel")
			}
		case "qemu":
			if s, ok := kv[1].(*starlarkstruct.Struct); ok {
				m.QEMU = &QEMUConfig{
					Machine: structString(s, "machine"),
					CPU:     structString(s, "cpu"),
					Memory:  structString(s, "memory"),
					Display: structString(s, "display"),
					Ports:   structStringList(s, "ports"),
				}
			}
		}
	}
	e.mu.Lock()
	e.machines[name] = m
	e.mu.Unlock()
	return starlark.None, nil
}

func (e *Engine) registerUnit(class string, kwargs []starlark.Tuple) (*Unit, error) {
	name := kwString(kwargs, "name")
	if name == "" {
		return nil, fmt.Errorf("%s() requires name", class)
	}

	// Allow Starlark to override class (e.g., image() class calls unit() with unit_class="image")
	cls := kwString(kwargs, "unit_class")
	if cls == "" {
		cls = class
	}

	r := &Unit{
		Name:              name,
		Version:           kwString(kwargs, "version"),
		Release:           kwInt(kwargs, "release"),
		Class:             cls,
		Scope:             kwString(kwargs, "scope"),
		Description:       kwString(kwargs, "description"),
		License:           kwString(kwargs, "license"),
		Distro:            kwString(kwargs, "distro"),
		Source:            kwString(kwargs, "source"),
		SHA256:            kwString(kwargs, "sha256"),
		APKChecksum:       kwString(kwargs, "apk_checksum"),
		PassthroughAPK:    kwString(kwargs, "passthrough_apk"),
		Tag:               kwString(kwargs, "tag"),
		Branch:            kwString(kwargs, "branch"),
		Patches:           kwStringList(kwargs, "patches"),
		Deps:              kwStringList(kwargs, "deps"),
		RuntimeDeps:       kwStringList(kwargs, "runtime_deps"),
		DistroDeps:        kwStringListMap(kwargs, "distro_deps"),
		DistroRuntimeDeps: kwStringListMap(kwargs, "distro_runtime_deps"),
		Container:         kwString(kwargs, "container"),
		ContainerArch:     kwString(kwargs, "container_arch"),
		Sandbox:           kwBool(kwargs, "sandbox"),
		Shell:             kwString(kwargs, "shell"),
		Provides:          kwStringList(kwargs, "provides"),
		Replaces:          kwStringList(kwargs, "replaces"),
		Services:          kwStringList(kwargs, "services"),
		Conffiles:         kwStringList(kwargs, "conffiles"),
		Environment:       kwStringMap(kwargs, "environment"),
		CacheDirs:         kwStringMap(kwargs, "cache_dirs"),
		Owners:            kwStringMap(kwargs, "owners"),
		Packages:          kwStringList(kwargs, "packages"),
	}

	// Parse tasks
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == "tasks" {
			if list, ok := kv[1].(*starlark.List); ok {
				r.Tasks = append(r.Tasks, ParseTaskList(list)...)
			}
		}
	}

	// Parse partitions if present
	if v := kwValue(kwargs, "boot"); v != nil {
		boot, err := parseBoot(v)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", class, name, err)
		}
		r.Boot = boot
	}

	for _, kv := range kwargs {
		k := string(kv[0].(starlark.String))
		if reservedUnitKwargs[k] {
			continue
		}
		v, err := starlarkToGo(kv[1])
		if err != nil {
			return nil, fmt.Errorf("%s() kwarg %q: %w", class, k, err)
		}
		if r.Extra == nil {
			r.Extra = make(map[string]any)
		}
		r.Extra[k] = v
	}

	r.Module = e.currentModule
	r.ModuleIndex = e.currentModuleIndex
	if e.currentFile != "" {
		r.DefinedIn = filepath.Dir(e.currentFile)
	}

	// prefer_modules pins are consulted at closure-walk time, not
	// here. Registration logic only needs to decide which module's
	// unit wins by module priority when two registrations collide on
	// the same name. The per-distro pins in proj.PreferModules then
	// shadow the priority choice at lookup time for the matching
	// distro only - alpine pins don't interfere with debian closures
	// and vice versa.
	e.mu.Lock()
	if existing, ok := e.units[name]; ok {
		// Same priority (same module, or both project root) → hard error.
		// Cross-priority collisions are shadows: highest priority wins, with
		// a stderr notice. Project priority is set strictly above any module
		// in loader.go, so project units always win.
		if r.ModuleIndex == existing.ModuleIndex {
			e.mu.Unlock()
			return nil, fmt.Errorf("unit %q already defined (first defined in %s)",
				name, moduleSource(existing.Module))
		}
		if r.ModuleIndex < existing.ModuleIndex {
			e.shadows = append(e.shadows, ShadowEvent{
				Unit:         name,
				WinnerModule: existing.Module,
				WinnerDir:    existing.DefinedIn,
				LoserModule:  r.Module,
				LoserDir:     r.DefinedIn,
			})
			e.mu.Unlock()
			if e.showShadows {
				fmt.Fprintf(os.Stderr,
					"notice: unit %q from %s is shadowed by %s\n",
					name, moduleSource(r.Module), moduleSource(existing.Module))
			}
			return existing, nil
		}
		// New unit has higher priority - replace, log the displacement.
		e.shadows = append(e.shadows, ShadowEvent{
			Unit:         name,
			WinnerModule: r.Module,
			WinnerDir:    r.DefinedIn,
			LoserModule:  existing.Module,
			LoserDir:     existing.DefinedIn,
		})
		if e.showShadows {
			fmt.Fprintf(os.Stderr,
				"notice: unit %q from %s shadows the same name from %s\n",
				name, moduleSource(r.Module), moduleSource(existing.Module))
		}
	}
	e.units[name] = r
	// Also store in the per-module catalog. Same-named units from
	// different modules coexist here (alpine.main's libssl3 doesn't
	// shadow debian.main's); the closure walker picks per consuming
	// distro at lookup time.
	e.storeByModule(r)
	e.mu.Unlock()

	return r, nil
}

// moduleSource formats a module name for diagnostic messages. The empty
// module string represents the project root.
func moduleSource(m string) string {
	if m == "" {
		return "project root"
	}
	return fmt.Sprintf("module %q", m)
}

func (e *Engine) fnUnit(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	_, err := e.registerUnit("unit", kwargs)
	return starlark.None, err
}

func (e *Engine) fnImage(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	_, err := e.registerUnit("image", kwargs)
	return starlark.None, err
}

// --- Task builtin ---

func fnTask(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name starlark.String
	if err := starlark.UnpackPositionalArgs("task", args, nil, 1, &name); err != nil {
		return nil, err
	}

	fields := starlark.StringDict{
		"name": name,
	}

	for _, kv := range kwargs {
		key := string(kv[0].(starlark.String))
		fields[key] = kv[1]
	}

	return starlarkstruct.FromStringDict(starlark.String("task"), fields), nil
}

// --- Custom commands ---

func fnArg(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("arg() requires a name")
	}
	name, ok := args[0].(starlark.String)
	if !ok {
		return nil, fmt.Errorf("arg() name must be a string")
	}
	d := starlark.StringDict{"name": name}
	for _, kv := range kwargs {
		d[string(kv[0].(starlark.String))] = kv[1]
	}
	return starlarkstruct.FromStringDict(starlark.String("arg"), d), nil
}

func (e *Engine) fnCommand(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	name := kwString(kwargs, "name")
	if name == "" {
		return nil, fmt.Errorf("command() requires name")
	}

	cmd := &Command{
		Name:        name,
		Description: kwString(kwargs, "description"),
		SourceFile:  thread.Name,
	}

	// Parse args list
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == "args" {
			if list, ok := kv[1].(*starlark.List); ok {
				iter := list.Iterate()
				defer iter.Done()
				var v starlark.Value
				for iter.Next(&v) {
					if s, ok := v.(*starlarkstruct.Struct); ok {
						a := CommandArg{
							Name:    structString(s, "name"),
							Help:    structString(s, "help"),
							Default: structString(s, "default"),
						}
						if rv, err := s.Attr("required"); err == nil {
							if b, ok := rv.(starlark.Bool); ok {
								a.Required = bool(b)
							}
						}
						if rv, err := s.Attr("type"); err == nil {
							if str, ok := rv.(starlark.String); ok && string(str) == "bool" {
								a.IsBool = true
							}
						}
						cmd.Args = append(cmd.Args, a)
					}
				}
			}
		}
	}

	e.mu.Lock()
	e.commands[name] = cmd
	e.mu.Unlock()

	return starlark.None, nil
}

func kwValue(kwargs []starlark.Tuple, key string) starlark.Value {
	for _, kv := range kwargs {
		if string(kv[0].(starlark.String)) == key {
			return kv[1]
		}
	}
	return nil
}

func parseBoot(v starlark.Value) (*Boot, error) {
	raw, err := starlarkToGo(v)
	if err != nil {
		return nil, fmt.Errorf("boot: %w", err)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("boot: want a dict, got %s", v.Type())
	}
	b := &Boot{
		Loader:   anyString(m["loader"]),
		Firmware: anyString(m["firmware"]),
		Features: anyStrings(m["features"]),
	}
	entries, _ := m["entries"].([]any)
	for _, e := range entries {
		em, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("boot: entries must be dicts")
		}
		initial, _ := em["initial"].(bool)
		b.Entries = append(b.Entries, BootEntry{
			Slot:    anyString(em["slot"]),
			Root:    anyString(em["root"]),
			Hash:    anyString(em["hash"]),
			Cmdline: anyString(em["cmdline"]),
			Initial: initial,
		})
	}
	return b, nil
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}

func anyStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func ParseBootEntries(list *starlark.List) ([]BootEntry, error) {
	d := starlark.NewDict(1)
	_ = d.SetKey(starlark.String("entries"), list)
	b, err := parseBoot(d)
	if err != nil {
		return nil, err
	}
	return b.Entries, nil
}
