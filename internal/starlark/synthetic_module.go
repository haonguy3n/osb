package starlark

// SyntheticModule is a module-priority entry whose units are materialized on
// demand instead of enumerated up front, so `alpine_feed(...)` and
// `apt_feed(...)` can absorb multi-thousand-entry upstream indices without
// allocating a *Unit per name.
//
// The loader otherwise treats it like any other module: module attribution,
// `prefer_modules` and source-tagged display all work. Only the timing
// differs - a real module registers every unit at load time, a synthetic one
// materializes on first Lookup from the closure walk.
//
// All fields are required; a nil Lookup or Names is a programmer error.
type SyntheticModule struct {
	// Name is the composed module name seen by the resolver and
	// prefer_modules, by convention `<parent>.<feed>` (e.g. `alpine.main`).
	Name string

	// Parent is the module whose MODULE.star declared this feed, used to
	// group feeds under it for display.
	Parent string

	// Suite is the release codename from apt_feed's `suite` kwarg (e.g.
	// "bookworm"); empty for alpine_feed. Project.SuiteForDistro matches it
	// against the feed's Distro, so a project with both a Debian and an
	// Ubuntu feed stamps the right codename per distro.
	Suite string

	// Distro is the distro this feed targets - apt_feed's `distro` kwarg
	// ("debian", "ubuntu"), or "alpine" for alpine_feed. Matches the
	// Distro tag stamped on the feed's materialized units; SuiteForDistro
	// uses it to pick this feed's suite for a given distro's build.
	Distro string

	// Release is the upstream release identifier when the feed declares
	// one as something other than an apt suite - alpine_feed's `branch`
	// (e.g. "v3.21"). apt feeds leave it empty and use Suite instead.
	// Project.BaseVersionForDistro surfaces Suite-or-Release as the base
	// version stamped into /etc/os-release.
	Release string

	// Priority is the resolver-priority index. Synthetic modules rank below
	// every real module under the "higher index wins" convention.
	Priority int

	// Lookup materializes a *Unit for name. (nil, nil) is a miss and the
	// resolver continues to the next module; (nil, err) is a parse or I/O
	// failure worth surfacing. Pointer identity across calls is not
	// required - the closure walk caches the first result.
	Lookup func(name string) (*Unit, error)

	// Names enumerates every name this module can materialize, for search.
	// Must NOT trigger Lookup or allocate units - decoupling catalog size
	// from working-set size is the point.
	Names func() []string
}

// RegisterSyntheticModule records sm for the loader to attach to the
// project's module list. Safe for concurrent use. A duplicate Name errors
// rather than silently overwriting the earlier registration.
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

// SyntheticModules returns registered synthetic modules in registration
// order, for the loader to assign Priority and attach to the project.
func (e *Engine) SyntheticModules() []*SyntheticModule {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Return a copy so callers iterating during further evaluation
	// don't race with concurrent registrations.
	out := make([]*SyntheticModule, len(e.syntheticModules))
	copy(out, e.syntheticModules)
	return out
}

// LookupInSynthetics walks the project's synthetic modules in priority
// order (highest Priority first) and returns the first *Unit whose name
// matches. Used by the closure walk (U7) when a referenced name isn't
// in proj.Units.
//
// The loader assigns Priority = -registration_index, so the slice is
// already in high-to-low priority order and a forward walk gives the
// correct precedence.
//
// Returns (nil, nil) when no synthetic module provides the name -
// distinguished from (nil, err) on parse/cache failure.
func LookupInSynthetics(synths []*SyntheticModule, name string) (*Unit, error) {
	for _, sm := range synths {
		u, err := sm.Lookup(name)
		if err != nil {
			return nil, err
		}
		if u != nil {
			return u, nil
		}
	}
	return nil, nil
}

// Error sentinels - exported via the dedicated types below so callers can
// distinguish them with errors.As/errors.Is when needed.
type syntheticModuleError string

func (e syntheticModuleError) Error() string { return string(e) }

const (
	errSyntheticModuleMissingName      = syntheticModuleError("synthetic module: Name is required")
	errSyntheticModuleMissingCallbacks = syntheticModuleError("synthetic module: Lookup and Names callbacks are required")
)

// duplicateSyntheticModuleError is returned when two alpine_feed (or
// equivalent) calls register the same Name in one project. Keeping the
// Name on the error type means downstream tests / TUI surfaces can
// format the message however they like instead of parsing strings.
type duplicateSyntheticModuleError struct{ Name string }

func (d *duplicateSyntheticModuleError) Error() string {
	return "synthetic module already registered: " + d.Name
}
