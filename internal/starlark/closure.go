package starlark

import (
	"fmt"

	"go.starlark.net/starlark"
)

func (e *Engine) fnResolveClosure(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("resolve_closure: takes exactly one positional argument (the list of root names)")
	}
	list, ok := args[0].(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("resolve_closure: argument must be a list of strings, got %s", args[0].Type())
	}
	roots := make([]string, 0, list.Len())
	iter := list.Iterate()
	defer iter.Done()
	var item starlark.Value
	for iter.Next(&item) {
		s, ok := item.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("resolve_closure: list element must be string, got %s", item.Type())
		}
		roots = append(roots, string(s))
	}
	effectiveDistro := kwString(kwargs, "distro")
	if effectiveDistro == "" {
		return nil, fmt.Errorf("resolve_closure: distro kwarg required (the consuming image's effective distro from the R20a/R21 cascade)")
	}

	ordered, err := e.closure(roots, effectiveDistro)
	if err != nil {
		return nil, fmt.Errorf("resolve_closure: %w", err)
	}
	vals := make([]starlark.Value, len(ordered))
	for i, n := range ordered {
		vals[i] = starlark.String(n)
	}
	return starlark.NewList(vals), nil
}

func (e *Engine) closure(roots []string, effectiveDistro string) ([]string, error) {
	if effectiveDistro == "" {
		panic("starlark: closure walker called with empty effectiveDistro (programmer error - R21a requires per-image scope)")
	}
	seen := make(map[string]bool, len(roots)*4)
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		u, err := e.lookupOrMaterialize(name, effectiveDistro)
		if err != nil {
			return nil, err
		}
		if u == nil {
			hint := ""
			if e.evalPhase == "units" {
				hint = " - an image defined under units/ evaluates before module units; move the .star file to images/"
			}
			return nil, fmt.Errorf("unresolved name %q (not in any module, no provider, or filtered by distro=%q)%s", name, effectiveDistro, hint)
		}
		seen[u.Name] = true
		for _, dep := range u.RuntimeDepsForDistro(effectiveDistro) {
			if seen[dep] {
				continue
			}
			queue = append(queue, dep)
		}
	}

	remaining := make([]string, 0, len(seen))
	for n := range seen {
		remaining = append(remaining, n)
	}
	emitted := make(map[string]bool, len(remaining))
	ordered := make([]string, 0, len(remaining))
	for range len(remaining) + 1 {
		next := remaining[:0]
		for _, name := range remaining {
			u, _ := e.lookupOrMaterialize(name, effectiveDistro)
			ready := true
			if u != nil {
				for _, dep := range u.RuntimeDepsForDistro(effectiveDistro) {
					resolved := e.resolveProvides(dep)
					if seen[resolved] && !emitted[resolved] {
						ready = false
						break
					}
				}
			}
			if ready {
				ordered = append(ordered, name)
				emitted[name] = true
			} else {
				next = append(next, name)
			}
		}
		if len(next) == len(remaining) {
			ordered = append(ordered, next...)
			return ordered, nil
		}
		remaining = next
		if len(remaining) == 0 {
			return ordered, nil
		}
	}
	return ordered, nil
}

func (e *Engine) lookupOrMaterialize(rawName, effectiveDistro string) (*Unit, error) {
	name := e.resolveProvidesForDistro(rawName, effectiveDistro)

	if effectiveDistro != "" && e.project != nil {
		if pins, ok := e.project.PreferModules[effectiveDistro]; ok {
			if pinned, ok := pins[name]; ok && pinned != "" {
				u, err := e.lookupInModule(name, pinned, effectiveDistro)
				if err != nil {
					return nil, err
				}
				if u != nil {
					return u, nil
				}
			}
		}
	}

	if u, ok := e.units[name]; ok {
		if visibleToDistro(u, effectiveDistro) {
			return u, nil
		}
		if alt := e.findVisibleByName(name, effectiveDistro); alt != nil {
			return alt, nil
		}
	}
	for _, sm := range e.syntheticModules {
		if sm.Distro != "" && effectiveDistro != "" && sm.Distro != effectiveDistro {
			continue
		}
		u, err := sm.Lookup(name)
		if err != nil {
			return nil, fmt.Errorf("synthetic module %q lookup %q: %w", sm.Name, name, err)
		}
		if u == nil {
			continue
		}
		if !visibleToDistro(u, effectiveDistro) {
			continue
		}
		e.mu.Lock()
		u.ModuleIndex = sm.Priority
		if _, ok := e.units[name]; !ok {
			e.units[name] = u
		}
		u.Module = sm.Name
		e.storeByModule(u)
		existing := e.units[name]
		e.mu.Unlock()
		if visibleToDistro(existing, effectiveDistro) && existing.Distro == effectiveDistro {
			return existing, nil
		}
		return u, nil
	}
	return nil, nil
}

func (e *Engine) findVisibleByName(name, effectiveDistro string) *Unit {
	e.mu.Lock()
	defer e.mu.Unlock()
	var best *Unit
	for _, byName := range e.unitsByModule {
		u, ok := byName[name]
		if !ok {
			continue
		}
		if !visibleToDistro(u, effectiveDistro) {
			continue
		}
		if best == nil || u.ModuleIndex > best.ModuleIndex {
			best = u
		}
	}
	return best
}

func (e *Engine) lookupInModule(name, moduleName, effectiveDistro string) (*Unit, error) {
	for _, sm := range e.syntheticModules {
		if sm.Name != moduleName {
			continue
		}
		u, err := sm.Lookup(name)
		if err != nil {
			return nil, fmt.Errorf("synthetic module %q lookup %q: %w", sm.Name, name, err)
		}
		if u == nil {
			return nil, nil
		}
		if !visibleToDistro(u, effectiveDistro) {
			return nil, nil
		}
		u.ModuleIndex = sm.Priority
		u.Module = sm.Name
		e.mu.Lock()
		e.storeByModule(u)
		e.mu.Unlock()
		return u, nil
	}
	if u := e.findInModuleByName(name, moduleName); u != nil && visibleToDistro(u, effectiveDistro) {
		return u, nil
	}
	return nil, nil
}

func (e *Engine) findInModuleByName(name, moduleName string) *Unit {
	e.mu.Lock()
	defer e.mu.Unlock()
	if byName, ok := e.unitsByModule[moduleName]; ok {
		return byName[name]
	}
	return nil
}

func visibleToDistro(u *Unit, effectiveDistro string) bool {
	if u == nil {
		return false
	}
	if effectiveDistro == "" {
		return true
	}
	return u.Distro == "" || u.Distro == effectiveDistro
}

func (e *Engine) resolveProvides(name string) string {
	if e.project == nil {
		return name
	}
	if mapped, ok := e.project.Provides[name]; ok && mapped != "" {
		return mapped
	}
	return name
}

func (e *Engine) resolveProvidesForDistro(name, effectiveDistro string) string {
	if e.project == nil {
		return name
	}
	if mapped := e.project.ResolveProvidesForDistro(name, effectiveDistro); mapped != "" {
		return mapped
	}
	return name
}
