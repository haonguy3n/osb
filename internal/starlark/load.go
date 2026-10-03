package starlark

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"go.starlark.net/starlark"
)

type loadResult struct {
	globals starlark.StringDict
	err     error
}

type loadCache struct {
	mu      sync.Mutex
	entries map[string]*loadResult
}

func newLoadCache() *loadCache {
	return &loadCache{entries: make(map[string]*loadResult)}
}

func (e *Engine) SetProjectRoot(root string) {
	e.projectRoot = root
}

func (e *Engine) SetModuleRoot(name, root string) {
	if e.moduleRoots == nil {
		e.moduleRoots = make(map[string]string)
	}
	e.moduleRoots[name] = root
}

func (e *Engine) makeLoadFunc(fromFile string) func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
	if e.loadCache == nil {
		e.loadCache = newLoadCache()
	}

	return func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
		absPath, err := e.resolveLoadPath(fromFile, module)
		if err != nil {
			return nil, err
		}

		e.loadCache.mu.Lock()
		if result, ok := e.loadCache.entries[absPath]; ok {
			e.loadCache.mu.Unlock()
			return result.globals, result.err
		}
		e.loadCache.entries[absPath] = nil
		e.loadCache.mu.Unlock()

		childThread := &starlark.Thread{Name: absPath}
		childThread.Load = e.makeLoadFunc(absPath)
		predeclared := e.builtins()

		globals, err := starlark.ExecFileOptions(fileOpts, childThread, absPath, nil, predeclared)

		result := &loadResult{globals: globals, err: err}
		e.loadCache.mu.Lock()
		e.loadCache.entries[absPath] = result
		e.loadCache.mu.Unlock()

		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", module, err)
		}
		return globals, nil
	}
}

func (e *Engine) rootForFile(file string) string {
	absFile, _ := filepath.Abs(file)
	for _, moduleRoot := range e.moduleRoots {
		absModule, _ := filepath.Abs(moduleRoot)
		if strings.HasPrefix(absFile, absModule+string(filepath.Separator)) {
			return absModule
		}
	}
	return e.projectRoot
}

func (e *Engine) resolveLoadPath(fromFile, module string) (string, error) {
	switch {
	case strings.HasPrefix(module, "@"):
		idx := strings.Index(module, "//")
		if idx < 0 {
			return "", fmt.Errorf("invalid module reference %q: expected @name//path", module)
		}
		moduleName := module[1:idx]
		relPath := module[idx+2:]
		root, ok := e.moduleRoots[moduleName]
		if !ok {
			return "", fmt.Errorf("unknown module %q in load(%q)", moduleName, module)
		}
		return filepath.Join(root, relPath), nil

	case strings.HasPrefix(module, "//"):
		root := e.rootForFile(fromFile)
		if root == "" {
			return "", fmt.Errorf("cannot resolve %q: no root for %s", module, fromFile)
		}
		return filepath.Join(root, module[2:]), nil

	default:
		dir := filepath.Dir(fromFile)
		return filepath.Join(dir, module), nil
	}
}
