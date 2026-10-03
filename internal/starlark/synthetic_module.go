package starlark

type SyntheticModule struct {
	Name string

	Parent string

	Suite string

	Distro string

	Release string

	Priority int

	Lookup func(name string) (*Unit, error)

	Names func() []string
}

func (e *Engine) RegisterSyntheticModule(sm *SyntheticModule) error {
	if sm == nil || sm.Name == "" {
		return errSyntheticModuleMissingName
	}
	if sm.Lookup == nil || sm.Names == nil {
		return errSyntheticModuleMissingCallbacks
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, existing := range e.syntheticModules {
		if existing.Name == sm.Name {
			return &duplicateSyntheticModuleError{Name: sm.Name}
		}
	}
	e.syntheticModules = append(e.syntheticModules, sm)
	return nil
}

func (e *Engine) SyntheticModules() []*SyntheticModule {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*SyntheticModule, len(e.syntheticModules))
	copy(out, e.syntheticModules)
	return out
}

type syntheticModuleError string

func (e syntheticModuleError) Error() string { return string(e) }

const (
	errSyntheticModuleMissingName      = syntheticModuleError("synthetic module: Name is required")
	errSyntheticModuleMissingCallbacks = syntheticModuleError("synthetic module: Lookup and Names callbacks are required")
)

type duplicateSyntheticModuleError struct{ Name string }

func (d *duplicateSyntheticModuleError) Error() string {
	return "synthetic module already registered: " + d.Name
}
