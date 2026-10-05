package starlark

import (
	"fmt"
	"path/filepath"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

var fileOpts = &syntax.FileOptions{}

type Engine struct {
	mu            sync.Mutex
	project       *Project
	machines      map[string]*Machine
	units         map[string]*Unit
	unitsByModule map[string]map[string]*Unit

	defaultPreferModules map[string]map[string]string

	currentModule      string
	currentModuleIndex int

	evalPhase string

	globals starlark.StringDict

	vars map[string]starlark.Value

	projectRoot string
	moduleRoots map[string]string
	loadCache   *loadCache

	currentFile string

	allowDuplicateProvides bool

	syntheticModules []*SyntheticModule

	extraBuiltins map[string]*starlark.Builtin

	activeArch string
}

func (e *Engine) SetExtraBuiltins(specs map[string]BuiltinFactory) {
	if e.extraBuiltins == nil {
		e.extraBuiltins = make(map[string]*starlark.Builtin, len(specs))
	}
	for name, factory := range specs {
		e.extraBuiltins[name] = factory(e)
	}
}

func (e *Engine) SetActiveArch(arch string) { e.activeArch = arch }

func (e *Engine) ActiveArch() string { return e.activeArch }

func NewEngine() *Engine {
	return &Engine{
		machines:      make(map[string]*Machine),
		units:         make(map[string]*Unit),
		unitsByModule: make(map[string]map[string]*Unit),
		vars:          make(map[string]starlark.Value),
	}
}

func (e *Engine) storeByModule(u *Unit) {
	if u == nil {
		return
	}
	mod := u.Module
	if e.unitsByModule[mod] == nil {
		e.unitsByModule[mod] = make(map[string]*Unit)
	}
	e.unitsByModule[mod][u.Name] = u
}

func (e *Engine) SetVar(name string, value starlark.Value) {
	e.vars[name] = value
}

func (e *Engine) Project() *Project             { return e.project }
func (e *Engine) Machines() map[string]*Machine { return e.machines }
func (e *Engine) Units() map[string]*Unit       { return e.units }

func (e *Engine) UnitsByModule() map[string]map[string]*Unit { return e.unitsByModule }

func (e *Engine) DefaultPreferModules() map[string]map[string]string { return e.defaultPreferModules }

func (e *Engine) SetCurrentModule(name string, index int) {
	e.currentModule = name
	e.currentModuleIndex = index
}

func (e *Engine) CurrentModule() string { return e.currentModule }

func (e *Engine) SetEvalPhase(phase string) { e.evalPhase = phase }

func (e *Engine) SetAllowDuplicateProvides(v bool) { e.allowDuplicateProvides = v }

func (e *Engine) ExecFile(path string) error {
	prev := e.currentFile
	e.currentFile = path
	defer func() { e.currentFile = prev }()
	if err := e.exec(path, nil); err != nil {
		return err
	}
	if absPath, _ := filepath.Abs(path); absPath != "" {
		if e.loadCache == nil {
			e.loadCache = newLoadCache()
		}
		e.loadCache.mu.Lock()
		e.loadCache.entries[absPath] = &loadResult{globals: e.globals}
		e.loadCache.mu.Unlock()
	}
	return nil
}

func (e *Engine) exec(filename string, src any) error {
	thread := &starlark.Thread{Name: filename}
	thread.Load = e.makeLoadFunc(filename)
	globals, err := starlark.ExecFileOptions(fileOpts, thread, filename, src, e.builtins())
	if err != nil {
		return fmt.Errorf("evaluating %s: %w", filename, err)
	}
	e.globals = globals
	return nil
}
