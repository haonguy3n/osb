package starlark

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func expandTransitiveDeps(initial []ModuleRef, projectRoot string) ([]ModuleRef, error) {

	seen := map[string]*moduleRecord{}
	depGraph := map[string][]string{}
	combined := append([]ModuleRef(nil), initial...)

	for i := range combined {
		id, err := canonicalIdentity(combined[i], projectRoot)
		if err != nil {
			return nil, err
		}
		seen[id] = &moduleRecord{ref: combined[i], projectLevel: true, id: id}
	}

	const maxRounds = 16

	for range maxRounds {

		var newRefs []ModuleRef
		for i := range combined {
			ref := combined[i]
			modulePath, _, ok := locateModulePath(ref, projectRoot)
			if !ok {
				continue
			}
			info := peekModuleInfo(modulePath)
			if info == nil {
				continue
			}
			parentName := info.Name
			if parentName == "" {
				parentName = pathBasename(ref)
			}
			for _, dep := range info.Deps {
				depID, err := canonicalIdentity(dep, projectRoot)
				if err != nil {
					return nil, fmt.Errorf("module %s deps: %w", parentName, err)
				}
				depName := dep.peekName(projectRoot)
				depGraph[parentName] = appendUnique(depGraph[parentName], depName)

				existing, alreadySeen := seen[depID]
				if alreadySeen {
					continue
				}

				if conflict := findNameConflict(seen, dep, depName); conflict != nil {
					if conflict.projectLevel {
						continue
					}
					return nil, fmt.Errorf(
						"module %q is declared by two transitive deps at incompatible refs: %s and %s (pin one explicitly at the project level)",
						depName, refDesc(conflict.ref), refDesc(dep))
				}

				seen[depID] = &moduleRecord{ref: dep, projectLevel: false, id: depID}
				newRefs = append(newRefs, dep)
				_ = existing
			}
		}
		if len(newRefs) == 0 {
			break
		}
		combined = append(combined, newRefs...)
	}

	if err := DetectCycles(depGraph); err != nil {
		return nil, err
	}
	return combined, nil
}

type moduleRecord struct {
	ref          ModuleRef
	projectLevel bool
	id           string
}

func canonicalIdentity(m ModuleRef, projectRoot string) (string, error) {
	if m.Local != "" {
		abs := m.Local
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(projectRoot, abs)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			resolved = abs
		}
		if m.Path != "" {
			resolved = filepath.Join(resolved, m.Path)
		}
		return "local:" + resolved, nil
	}
	url := strings.TrimSuffix(m.URL, ".git")
	ref := m.Ref
	return "git:" + url + "@" + ref + "#" + m.Path, nil
}

func findNameConflict(seen map[string]*moduleRecord, candidate ModuleRef, depName string) *moduleRecord {
	if depName == "" {
		return nil
	}
	candidateName := depName
	for _, rec := range seen {
		existingName := pathBasename(rec.ref)
		if existingName == candidateName {
			cID, _ := canonicalIdentity(rec.ref, "")
			candID, _ := canonicalIdentity(candidate, "")
			if cID != candID {
				return rec
			}
		}
	}
	return nil
}

func (m ModuleRef) peekName(_ string) string {
	return pathBasename(m)
}

func refDesc(m ModuleRef) string {
	if m.Local != "" {
		return fmt.Sprintf("local=%s", m.Local)
	}
	if m.Ref != "" {
		return fmt.Sprintf("%s @ %s", m.URL, m.Ref)
	}
	return m.URL
}

func appendUnique(ss []string, s string) []string {
	if slices.Contains(ss, s) {
		return ss
	}
	return append(ss, s)
}
