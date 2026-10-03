package starlark

import (
	"fmt"
	"iter"

	"go.starlark.net/starlark"
)

type Project struct {
	Name     string
	Version  string
	Defaults Defaults
	Cache    CacheConfig
	Sources  SourcesConfig
	Modules  []ModuleRef
	Machines map[string]*Machine

	UnitsByModule map[string]map[string]*Unit

	DistroViews map[string]map[string]*Unit

	DefaultDistro string

	DefaultDistroOverride string

	PreferModules map[string]map[string]string

	Provides map[string]string

	SigningKey string

	ResolvedModules []ResolvedModule

	Diagnostics Diagnostics

	SyntheticModules []*SyntheticModule
}

type ResolvedModule struct {
	Name      string
	URL       string
	Ref       string
	Path      string
	Local     string
	Dir       string
	CloneDir  string
	Available bool
}

type Diagnostics struct {
	Shadows []ShadowEvent

	DuplicateProvides []ProvidesEvent
}

type ShadowEvent struct {
	Unit         string
	WinnerModule string
	WinnerDir    string
	LoserModule  string
	LoserDir     string
}

type ProvidesEvent struct {
	Virtual string
	Active  string
	Others  []string
}

type Defaults struct {
	Machine string
	Image   string
}

type CacheConfig struct {
	Path      string
	Remote    []CacheRemote
	Retention int
	Signing   string
}

type CacheRemote struct {
	Name     string
	Bucket   string
	Endpoint string
	Region   string
	Prefix   string
}

type SourcesConfig struct {
	GoProxy       string
	CargoRegistry string
	NpmRegistry   string
	PypiMirror    string
}

type ModuleRef struct {
	URL   string
	Ref   string
	Path  string
	Local string
}

type ModuleInfo struct {
	Name        string
	Description string
	Deps        []ModuleRef
}

type Machine struct {
	Name           string
	Arch           string
	Description    string
	Firmware       string
	Bootloader     string
	Console        string
	Cmdline        string
	Kernel         map[string]string
	Packages       []string
	DistroPackages map[string][]string
	QEMU           *QEMUConfig
}

const (
	FirmwareUEFI = "uefi"
	FirmwareBIOS = "bios"
)

type QEMUConfig struct {
	Machine string
	CPU     string
	Memory  string
	Display string
	Ports   []string
}

func (p *Project) EffectiveDistroForImage(imageName string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("EffectiveDistroForImage: nil project")
	}
	projDistro := p.DefaultDistroOverride
	if projDistro == "" {
		projDistro = p.DefaultDistro
	}
	var u *Unit
	if projDistro != "" {
		u = p.LookupUnit(projDistro, imageName)
	}
	if u == nil {
		u = p.AnyUnit(imageName)
	}
	if u == nil {
		return "", fmt.Errorf("EffectiveDistroForImage: unit %q not found", imageName)
	}
	if u.Class != "image" {
		return "", fmt.Errorf("EffectiveDistroForImage: unit %q is not an image (class=%q)", imageName, u.Class)
	}
	if u.Distro != "" {
		return u.Distro, nil
	}
	if p.DefaultDistroOverride != "" {
		return p.DefaultDistroOverride, nil
	}
	if p.DefaultDistro != "" {
		return p.DefaultDistro, nil
	}
	return "", fmt.Errorf("image %q has no distro and project has no defaults.distro (set distro on the image or defaults.distro on project)", imageName)
}

func (p *Project) LookupUnit(distro, name string) *Unit {
	if p == nil {
		return nil
	}
	if view, ok := p.DistroViews[distro]; ok {
		if u, ok := view[name]; ok {
			return u
		}
		return nil
	}
	if len(p.DistroViews) > 0 {
		return nil
	}
	return p.AnyUnit(name)
}

func (p *Project) AnyUnit(name string) *Unit {
	if p == nil {
		return nil
	}
	var best *Unit
	for _, byName := range p.UnitsByModule {
		u, ok := byName[name]
		if !ok {
			continue
		}
		if best == nil || u.ModuleIndex > best.ModuleIndex {
			best = u
		}
	}
	return best
}

func (p *Project) AllUnits() iter.Seq2[string, *Unit] {
	return func(yield func(string, *Unit) bool) {
		if p == nil {
			return
		}
		for _, byName := range p.UnitsByModule {
			for name, u := range byName {
				if !yield(name, u) {
					return
				}
			}
		}
	}
}

func (p *Project) ResolveProvidesForDistro(virtual, effectiveDistro string) string {
	if p == nil || virtual == "" {
		return ""
	}
	if effectiveDistro != "" {
		if u := p.LookupUnit(effectiveDistro, virtual); u != nil && (u.Distro == effectiveDistro || u.Distro == "") {
			if u.Distro == effectiveDistro {
				return u.Name
			}
		}
	}
	if effectiveDistro != "" {
		for _, byName := range p.UnitsByModule {
			for _, u := range byName {
				if u.Distro != effectiveDistro {
					continue
				}
				for _, v := range u.Provides {
					if v == virtual {
						return u.Name
					}
				}
			}
		}
	}
	return p.Provides[virtual]
}

func (p *Project) EffectiveDistro() (string, error) {
	if p == nil {
		return "", fmt.Errorf("EffectiveDistro: nil project")
	}
	if p.DefaultDistroOverride != "" {
		return p.DefaultDistroOverride, nil
	}
	if p.DefaultDistro != "" {
		return p.DefaultDistro, nil
	}
	return "", fmt.Errorf("project has no defaults.distro (set defaults.distro on project)")
}

var AptFamilyDistros = map[string]bool{
	"debian": true,
	"ubuntu": true,
}

func IsAptFamily(distro string) bool {
	return AptFamilyDistros[distro]
}

func (p *Project) SuiteForDistro(distro string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("SuiteForDistro: nil project")
	}
	suite := ""
	for _, sm := range p.SyntheticModules {
		if sm == nil || sm.Suite == "" || sm.Distro != distro {
			continue
		}
		if suite == "" {
			suite = sm.Suite
		} else if sm.Suite != suite {
			return "", fmt.Errorf("project declares multiple %s suites (%q and %q); one suite per distro", distro, suite, sm.Suite)
		}
	}
	if suite == "" {
		return "", fmt.Errorf("no apt_feed declares a suite for distro %q; an %s image build needs an apt_feed(distro=%q, ...) in a module", distro, distro, distro)
	}
	return suite, nil
}

func (p *Project) BaseVersionForDistro(distro string) string {
	if p == nil {
		return ""
	}
	for _, sm := range p.SyntheticModules {
		if sm == nil || sm.Distro != distro {
			continue
		}
		if sm.Suite != "" {
			return sm.Suite
		}
		if sm.Release != "" {
			return sm.Release
		}
	}
	return ""
}

func (m *Machine) QEMUPorts() []string {
	if m == nil || m.QEMU == nil {
		return nil
	}
	return m.QEMU.Ports
}

type Unit struct {
	Name        string
	Version     string
	Release     int
	Class       string
	Scope       string
	Description string
	License     string

	Distro string

	Source      string
	SHA256      string
	APKChecksum string
	Tag         string
	Branch      string
	Patches     []string

	PassthroughAPK string

	PassthroughDeb string

	Deps        []string
	RuntimeDeps []string

	DistroDeps        map[string][]string
	DistroRuntimeDeps map[string][]string

	Container     string
	ContainerArch string
	Sandbox       bool
	Shell         string
	Tasks         []Task
	Provides      []string
	Replaces      []string
	Module        string
	ModuleIndex   int
	DefinedIn     string

	Services    []string
	Conffiles   []string
	Environment map[string]string
	CacheDirs   map[string]string

	Owners map[string]string

	Packages []string
	Boot     *Boot

	Extra map[string]any
}

type Boot struct {
	Loader   string
	Firmware string
	Features []string
	Entries  []BootEntry
}

type BootEntry struct {
	Slot    string
	Root    string
	Hash    string
	Cmdline string
	Initial bool
}

func (b *Boot) Has(feature string) bool {
	if b == nil {
		return false
	}
	for _, f := range b.Features {
		if f == feature {
			return true
		}
	}
	return false
}

type Step struct {
	Command string
	Fn      starlark.Callable
	Install *InstallStep
}

type InstallStep struct {
	Kind    string
	Src     string
	Dest    string
	Mode    int
	BaseDir string
}

type Task struct {
	Name      string
	Container string
	Steps     []Step
}

type Command struct {
	Name        string
	Description string
	Args        []CommandArg
	RunFn       string
	SourceFile  string
}

type CommandArg struct {
	Name     string
	Help     string
	Default  string
	Required bool
	IsBool   bool
}

var validArchitectures = map[string]bool{
	"arm64":  true,
	"x86_64": true,
}

func (u *Unit) DepsForDistro(distro string) []string {
	if u == nil {
		return nil
	}
	if distro == "" || len(u.DistroDeps) == 0 {
		return u.Deps
	}
	extra, ok := u.DistroDeps[distro]
	if !ok || len(extra) == 0 {
		return u.Deps
	}
	out := make([]string, 0, len(u.Deps)+len(extra))
	out = append(out, u.Deps...)
	out = append(out, extra...)
	return out
}

func (u *Unit) RuntimeDepsForDistro(distro string) []string {
	if u == nil {
		return nil
	}
	if distro == "" || len(u.DistroRuntimeDeps) == 0 {
		return u.RuntimeDeps
	}
	extra, ok := u.DistroRuntimeDeps[distro]
	if !ok || len(extra) == 0 {
		return u.RuntimeDeps
	}
	out := make([]string, 0, len(u.RuntimeDeps)+len(extra))
	out = append(out, u.RuntimeDeps...)
	out = append(out, extra...)
	return out
}
