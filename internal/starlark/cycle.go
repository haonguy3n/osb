package starlark

import (
	"fmt"
	"sort"
	"strings"
)

func DetectCycles(graph map[string][]string) error {
	roots := make([]string, 0, len(graph))
	for n := range graph {
		roots = append(roots, n)
	}
	sort.Strings(roots)

	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(graph))

	var stack []string
	var visit func(node string) error
	visit = func(node string) error {
		switch state[node] {
		case visiting:
			start := 0
			for i, n := range stack {
				if n == node {
					start = i
					break
				}
			}
			path := append(append([]string(nil), stack[start:]...), node)
			return &CycleError{Path: path}
		case visited:
			return nil
		}
		state[node] = visiting
		stack = append(stack, node)
		deps := append([]string(nil), graph[node]...)
		sort.Strings(deps)
		for _, dep := range deps {
			if _, ok := graph[dep]; !ok {
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = visited
		return nil
	}
	for _, r := range roots {
		if state[r] == visited {
			continue
		}
		if err := visit(r); err != nil {
			return err
		}
	}
	return nil
}

type CycleError struct{ Path []string }

func (e *CycleError) Error() string {
	if len(e.Path) == 0 {
		return "module dep cycle (empty path)"
	}
	return fmt.Sprintf("module dep cycle: %s", strings.Join(e.Path, " → "))
}
