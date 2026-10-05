package resolve

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/anhhao17/osb/internal/source"
	osbstar "github.com/anhhao17/osb/internal/starlark"
	"go.starlark.net/starlark"
)

func hashStringMap(h io.Writer, label string, m map[string]string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + m[k]
	}
	fmt.Fprintf(h, "%s:%s\n", label, strings.Join(parts, ","))
}

func UnitHash(unit *osbstar.Unit, arch string, depHashes map[string]string, srcInputs, effectiveDistro string) string {
	h := sha256.New()

	fmt.Fprintf(h, "name:%s\n", unit.Name)
	fmt.Fprintf(h, "version:%s\n", unit.Version)
	fmt.Fprintf(h, "release:%d\n", unit.Release)
	fmt.Fprintf(h, "class:%s\n", unit.Class)
	fmt.Fprintf(h, "scope:%s\n", unit.Scope)
	fmt.Fprintf(h, "arch:%s\n", arch)

	fmt.Fprintf(h, "description:%s\n", unit.Description)
	fmt.Fprintf(h, "license:%s\n", unit.License)

	fmt.Fprintf(h, "source:%s\n", unit.Source)
	if dir := source.LocalDir(unit); dir != "" {
		fmt.Fprintf(h, "source-tree:%s\n", source.HashLocalDir(dir))
	}
	fmt.Fprintf(h, "sha256:%s\n", unit.SHA256)
	if unit.APKChecksum != "" {
		fmt.Fprintf(h, "apk_checksum:%s\n", unit.APKChecksum)
	}
	fmt.Fprintf(h, "passthrough_apk:%s\n", unit.PassthroughAPK)
	fmt.Fprintf(h, "tag:%s\n", unit.Tag)
	fmt.Fprintf(h, "branch:%s\n", unit.Branch)
	fmt.Fprintf(h, "patches:%s\n", strings.Join(unit.Patches, "|"))

	if srcInputs != "" {
		fmt.Fprintf(h, "src_state:%s\n", srcInputs)
	}
	if effectiveDistro != "" {
		fmt.Fprintf(h, "effective_distro:%s\n", effectiveDistro)
	}

	for _, t := range unit.Tasks {
		fmt.Fprintf(h, "task:%s:%s\n", t.Name, t.Container)
		for _, s := range t.Steps {
			if s.Command != "" {
				fmt.Fprintf(h, "step:cmd:%s\n", s.Command)
			}
			if s.Fn != nil {
				fmt.Fprintf(h, "step:fn:%s\n", s.Fn.Name())
				if f, ok := s.Fn.(*starlark.Function); ok {
					hashStarlarkSource(h, f.Position().Filename(), map[string]bool{})
				}
			}
			if s.Install != nil {
				fmt.Fprintf(h, "step:install:%s:%s:%s:%o:%s\n",
					s.Install.Kind, s.Install.Src, s.Install.Dest,
					s.Install.Mode, s.Install.BaseDir)
				if src := filepath.Join(s.Install.BaseDir, s.Install.Src); src != "" {
					if data, err := os.ReadFile(src); err == nil {
						sum := sha256.Sum256(data)
						fmt.Fprintf(h, "step:install:src-sha256:%x\n", sum[:])
					}
				}
			}
		}
	}
	fmt.Fprintf(h, "container:%s\n", unit.Container)
	fmt.Fprintf(h, "container_arch:%s\n", unit.ContainerArch)
	fmt.Fprintf(h, "sandbox:%v\n", unit.Sandbox)
	fmt.Fprintf(h, "shell:%s\n", unit.Shell)
	fmt.Fprintf(h, "provides:%s\n", strings.Join(unit.Provides, ","))
	fmt.Fprintf(h, "replaces:%s\n", strings.Join(unit.Replaces, ","))
	fmt.Fprintf(h, "runtime_deps:%s\n", strings.Join(unit.RuntimeDeps, ","))
	if effectiveDistro != "" {
		if extra := unit.DistroDeps[effectiveDistro]; len(extra) > 0 {
			fmt.Fprintf(h, "distro_deps:%s:%s\n", effectiveDistro, strings.Join(extra, ","))
		}
		if extra := unit.DistroRuntimeDeps[effectiveDistro]; len(extra) > 0 {
			fmt.Fprintf(h, "distro_runtime_deps:%s:%s\n", effectiveDistro, strings.Join(extra, ","))
		}
	}
	fmt.Fprintf(h, "services:%s\n", strings.Join(unit.Services, ","))
	fmt.Fprintf(h, "conffiles:%s\n", strings.Join(unit.Conffiles, ","))
	hashStringMap(h, "environment", unit.Environment)
	if len(unit.Owners) > 0 {
		hashStringMap(h, "owners", unit.Owners)
	}

	if len(unit.Extra) > 0 {
		if b, err := json.Marshal(sortedMap(unit.Extra)); err == nil {
			fmt.Fprintf(h, "extra:%s\n", b)
		}
	}

	if unit.DefinedIn != "" {
		filesDir := filepath.Join(unit.DefinedIn, unit.Name)
		hashFilesDir(h, filesDir)
	}

	deps := append([]string{}, unit.DepsForDistro(effectiveDistro)...)
	if unit.Class == "image" {
		deps = append(deps, unit.Packages...)
	}
	sort.Strings(deps)
	for _, dep := range deps {
		if dh, ok := depHashes[dep]; ok {
			fmt.Fprintf(h, "dep:%s:%s\n", dep, dh)
		}
	}

	if unit.Class == "image" {
		pkgs := append([]string{}, unit.Packages...)
		sort.Strings(pkgs)
		fmt.Fprintf(h, "packages:%s\n", strings.Join(pkgs, ","))
		if unit.Boot != nil {
			if b, err := json.Marshal(unit.Boot); err == nil {
				fmt.Fprintf(h, "boot:%s\n", b)
			}
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

func ComputeAllHashes(dag *DAG, arch, machine string, srcInputs func(*osbstar.Unit) string, effectiveDistro string) (map[string]string, error) {
	order, err := dag.TopologicalSort()
	if err != nil {
		return nil, err
	}

	hashes := make(map[string]string, len(order))
	for _, name := range order {
		node := dag.Nodes[name]
		unitArch := arch
		if node.Unit.Scope == "machine" {
			unitArch = arch + ":" + machine
		}
		var src string
		if srcInputs != nil {
			src = srcInputs(node.Unit)
		}
		hashes[name] = UnitHash(node.Unit, unitArch, hashes, src, effectiveDistro)
	}

	return hashes, nil
}

func sortedMap(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = sortedMap(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = sortedMap(e)
		}
		return out
	default:
		return v
	}
}

func hashFilesDir(h io.Writer, dir string) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	var paths []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		paths = append(paths, p)
		return nil
	})
	sort.Strings(paths)
	for _, p := range paths {
		rel, _ := filepath.Rel(dir, p)
		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(content)
		fmt.Fprintf(h, "file:%s:%x\n", rel, sum[:])
	}
}

func hashStarlarkSource(h io.Writer, file string, seen map[string]bool) {
	if seen[file] {
		return
	}
	seen[file] = true
	data, err := os.ReadFile(file)
	if err != nil {
		return
	}
	fmt.Fprintf(h, "fn-src:%s:%x\n", filepath.Base(file), sha256.Sum256(data))
	for _, m := range loadRE.FindAllStringSubmatch(string(data), -1) {
		hashStarlarkSource(h, filepath.Join(filepath.Dir(file), m[1]), seen)
	}
}

var loadRE = regexp.MustCompile(`load\("(?:@[a-z-]+)?//classes/([^"/]+\.star)"`)
