package starlark

import "testing"

func TestBuildDistroViews_CrossDistroCoexistence(t *testing.T) {
	proj := &Project{
		DefaultDistro: "alpine",
		UnitsByModule: map[string]map[string]*Unit{
			"alpine.main": {
				"libssl3": {Name: "libssl3", Distro: "alpine", Module: "alpine.main", ModuleIndex: -1},
			},
			"debian.main": {
				"libssl3": {Name: "libssl3", Distro: "debian", Module: "debian.main", ModuleIndex: -2},
			},
		},
	}
	views := buildDistroViews(proj)
	if got := views["alpine"]["libssl3"]; got == nil || got.Distro != "alpine" {
		t.Errorf("alpine view should hold alpine variant; got %+v", got)
	}
	if got := views["debian"]["libssl3"]; got == nil || got.Distro != "debian" {
		t.Errorf("debian view should hold debian variant; got %+v", got)
	}
}

func TestBuildDistroViews_UntaggedSatisfiesBoth(t *testing.T) {
	proj := &Project{
		DefaultDistro: "alpine",
		PreferModules: map[string]map[string]string{"debian": {}},
		UnitsByModule: map[string]map[string]*Unit{
			"module-core": {
				"openssl": {Name: "openssl", Module: "module-core", ModuleIndex: 5},
			},
		},
	}
	views := buildDistroViews(proj)
	for _, d := range []string{"alpine", "debian"} {
		if got := views[d]["openssl"]; got == nil || got.Distro != "" {
			t.Errorf("%s view should hold untagged openssl; got %+v", d, got)
		}
	}
}

func TestBuildDistroViews_PreferModulesPin(t *testing.T) {
	proj := &Project{
		DefaultDistro: "alpine",
		PreferModules: map[string]map[string]string{
			"alpine": {"xz": "alpine.main"},
		},
		UnitsByModule: map[string]map[string]*Unit{
			"module-core": {
				"xz": {Name: "xz", Module: "module-core", ModuleIndex: 5},
			},
			"alpine.main": {
				"xz": {Name: "xz", Distro: "alpine", Module: "alpine.main", ModuleIndex: -1},
			},
		},
	}
	views := buildDistroViews(proj)
	if got := views["alpine"]["xz"]; got == nil || got.Module != "alpine.main" {
		t.Errorf("alpine view should hold pinned alpine.main xz; got %+v", got)
	}
}

func TestBuildDistroViews_LookupUnit(t *testing.T) {
	proj := &Project{
		DefaultDistro: "alpine",
		UnitsByModule: map[string]map[string]*Unit{
			"module-core": {
				"openssl": {Name: "openssl", Module: "module-core"},
			},
		},
	}
	proj.DistroViews = buildDistroViews(proj)
	if got := proj.LookupUnit("alpine", "openssl"); got == nil {
		t.Errorf("LookupUnit(alpine, openssl) should return unit, got nil")
	}
	if got := proj.LookupUnit("nonexistent", "openssl"); got != nil {
		t.Errorf("LookupUnit(nonexistent, openssl) should return nil, got %+v", got)
	}
	if got := proj.LookupUnit("alpine", "missing"); got != nil {
		t.Errorf("LookupUnit(alpine, missing) should return nil, got %+v", got)
	}
}

func TestProject_AllUnits(t *testing.T) {
	proj := &Project{
		UnitsByModule: map[string]map[string]*Unit{
			"module-core": {
				"openssl": {Name: "openssl"},
				"zlib":    {Name: "zlib"},
			},
			"alpine.main": {
				"libssl3": {Name: "libssl3", Distro: "alpine"},
			},
		},
	}
	seen := map[string]int{}
	for name := range proj.AllUnits() {
		seen[name]++
	}
	if seen["openssl"] != 1 || seen["zlib"] != 1 || seen["libssl3"] != 1 {
		t.Errorf("AllUnits should yield each name once; got %+v", seen)
	}
}
