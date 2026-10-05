package resolve

import (
	"fmt"
	"sort"
	"strings"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

type DAG struct {
	Nodes map[string]*Node
}

type Node struct {
	Unit  *osbstar.Unit
	Deps  []string
	Rdeps []string
}

func BuildDAG(proj *osbstar.Project, effectiveDistro string) (*DAG, error) {
	dag := &DAG{Nodes: make(map[string]*Node)}

	var units map[string]*osbstar.Unit
	allowMissingDeps := false
	if effectiveDistro != "" && proj.DistroViews != nil {
		if view, ok := proj.DistroViews[effectiveDistro]; ok {
			units = view
			allowMissingDeps = true
		}
	}
	if units == nil {
		units = map[string]*osbstar.Unit{}
		for name, u := range proj.AllUnits() {
			if _, ok := units[name]; !ok {
				units[name] = u
			}
		}
	}

	for name, unit := range units {
		resolveDistro := effectiveDistro
		if resolveDistro == "" {
			resolveDistro = unit.Distro
		}
		deps := append([]string{}, unit.DepsForDistro(resolveDistro)...)
		if unit.Class == "image" {
			deps = append(deps, unit.Packages...)
		}
		deps = appendContainerDeps(deps, proj, units, unit, resolveDistro)
		if unit.Class != "image" {
			deps = appendRuntimeClosureOfDeps(deps, units, name, resolveDistro)
		}
		dag.Nodes[name] = &Node{
			Unit: unit,
			Deps: resolveDeps(deps, proj, resolveDistro),
		}
	}

	for name, node := range dag.Nodes {
		filtered := node.Deps[:0]
		for _, dep := range node.Deps {
			target, ok := dag.Nodes[dep]
			if !ok {
				if allowMissingDeps {
					continue
				}
				return nil, fmt.Errorf("unit %q depends on %q, which does not exist", name, dep)
			}
			filtered = append(filtered, dep)
			target.Rdeps = append(target.Rdeps, name)
		}
		node.Deps = filtered
	}

	for _, node := range dag.Nodes {
		sort.Strings(node.Rdeps)
	}

	return dag, nil
}

func resolveDeps(deps []string, proj *osbstar.Project, distro string) []string {
	out := make([]string, 0, len(deps))
	seen := make(map[string]bool, len(deps))
	for _, d := range deps {
		resolved := proj.ResolveProvidesForDistro(d, distro)
		if resolved == "" {
			resolved = d
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		out = append(out, resolved)
	}
	return out
}

func appendRuntimeClosureOfDeps(deps []string, units map[string]*osbstar.Unit, self, distro string) []string {
	seen := make(map[string]bool, len(deps))
	for _, d := range deps {
		seen[d] = true
	}
	queue := append([]string{}, deps...)
	for i := 0; i < len(queue); i++ {
		u, ok := units[queue[i]]
		if !ok {
			continue
		}
		for _, r := range u.RuntimeDepsForDistro(distro) {
			if r == self || seen[r] {
				continue
			}
			seen[r] = true
			if _, ok := units[r]; !ok {
				continue
			}
			deps = append(deps, r)
			queue = append(queue, r)
		}
	}
	return deps
}

func appendContainerDeps(deps []string, proj *osbstar.Project, units map[string]*osbstar.Unit, unit *osbstar.Unit, distro string) []string {
	seen := make(map[string]bool, len(deps))
	for _, d := range deps {
		seen[d] = true
	}
	add := func(container string) {
		if container == "" || container == unit.Name {
			return
		}
		if strings.Contains(container, ":") || strings.Contains(container, "/") {
			return
		}
		if resolved := proj.ResolveProvidesForDistro(container, distro); resolved != "" {
			container = resolved
		}
		if _, ok := units[container]; !ok {
			return
		}
		if seen[container] {
			return
		}
		seen[container] = true
		deps = append(deps, container)
	}
	add(unit.Container)
	for _, t := range unit.Tasks {
		add(t.Container)
	}
	return deps
}

func (d *DAG) TopologicalSort() ([]string, error) {
	inDegree := make(map[string]int)
	for name := range d.Nodes {
		inDegree[name] = 0
	}
	for _, node := range d.Nodes {
		for _, dep := range node.Deps {
			inDegree[dep]++
		}
	}

	inDegree = make(map[string]int)
	for name, node := range d.Nodes {
		inDegree[name] = len(node.Deps)
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	var order []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		order = append(order, name)

		node := d.Nodes[name]
		for _, rdep := range node.Rdeps {
			inDegree[rdep]--
			if inDegree[rdep] == 0 {
				queue = append(queue, rdep)
				sort.Strings(queue)
			}
		}
	}

	if len(order) != len(d.Nodes) {
		var cycleNodes []string
		for name, deg := range inDegree {
			if deg > 0 {
				cycleNodes = append(cycleNodes, name)
			}
		}
		sort.Strings(cycleNodes)
		return nil, fmt.Errorf("dependency cycle detected involving: %s", strings.Join(cycleNodes, ", "))
	}

	return order, nil
}

func (d *DAG) DepsOf(name string) ([]string, error) {
	if _, ok := d.Nodes[name]; !ok {
		return nil, fmt.Errorf("unit %q not found", name)
	}

	visited := make(map[string]bool)
	var result []string

	var walk func(n string)
	walk = func(n string) {
		node := d.Nodes[n]
		for _, dep := range node.Deps {
			if !visited[dep] {
				visited[dep] = true
				result = append(result, dep)
				walk(dep)
			}
		}
	}

	walk(name)
	sort.Strings(result)
	return result, nil
}

func (d *DAG) TransitiveDeps(name string) []string {
	visited := map[string]bool{}
	var result []string
	var walk func(n string)
	walk = func(n string) {
		if visited[n] {
			return
		}
		visited[n] = true
		if node, ok := d.Nodes[n]; ok {
			for _, dep := range node.Deps {
				walk(dep)
			}
		}
		result = append(result, n)
	}
	if node, ok := d.Nodes[name]; ok {
		for _, dep := range node.Deps {
			walk(dep)
		}
	}
	return result
}

func (d *DAG) RdepsOf(name string) ([]string, error) {
	if _, ok := d.Nodes[name]; !ok {
		return nil, fmt.Errorf("unit %q not found", name)
	}

	visited := make(map[string]bool)
	var result []string

	var walk func(n string)
	walk = func(n string) {
		node := d.Nodes[n]
		for _, rdep := range node.Rdeps {
			if !visited[rdep] {
				visited[rdep] = true
				result = append(result, rdep)
				walk(rdep)
			}
		}
	}

	walk(name)
	sort.Strings(result)
	return result, nil
}
