package starlark

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"

	"go.starlark.net/starlark"
)

type InstallStepValue struct {
	Kind    string
	Src     string
	Dest    string
	Mode    int
	BaseDir string
}

var _ starlark.Value = (*InstallStepValue)(nil)

func (s *InstallStepValue) String() string {
	fn := "install_file"
	if s.Kind == "template" {
		fn = "install_template"
	}
	return fmt.Sprintf("%s(%q, %q, mode=0o%o)", fn, s.Src, s.Dest, s.Mode)
}

func (*InstallStepValue) Type() string         { return "InstallStep" }
func (*InstallStepValue) Freeze()              {}
func (*InstallStepValue) Truth() starlark.Bool { return starlark.True }

func (s *InstallStepValue) Hash() (uint32, error) {
	h := fnv.New32a()
	h.Write([]byte(s.Kind))
	h.Write([]byte{0})
	h.Write([]byte(s.Src))
	h.Write([]byte{0})
	h.Write([]byte(s.Dest))
	h.Write([]byte{0})
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(s.Mode))
	h.Write(buf[:])
	return h.Sum32(), nil
}

func fnInstallFile(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return buildInstallStep(thread, "install_file", "file", args, kwargs, 0o644)
}

func fnInstallTemplate(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return buildInstallStep(thread, "install_template", "template", args, kwargs, 0o644)
}

func buildInstallStep(thread *starlark.Thread, name, kind string, args starlark.Tuple, kwargs []starlark.Tuple, defMode int) (starlark.Value, error) {
	var src, dest starlark.String
	if err := starlark.UnpackPositionalArgs(name, args, nil, 2, &src, &dest); err != nil {
		return nil, err
	}
	mode := defMode
	for _, kv := range kwargs {
		k := string(kv[0].(starlark.String))
		if k != "mode" {
			return nil, fmt.Errorf("%s: unexpected kwarg %q", name, k)
		}
		n, ok := kv[1].(starlark.Int)
		if !ok {
			return nil, fmt.Errorf("%s: mode must be int, got %s", name, kv[1].Type())
		}
		v, ok := n.Int64()
		if !ok {
			return nil, fmt.Errorf("%s: mode out of range", name)
		}
		mode = int(v)
	}
	return &InstallStepValue{
		Kind:    kind,
		Src:     string(src),
		Dest:    string(dest),
		Mode:    mode,
		BaseDir: callerBaseDir(thread),
	}, nil
}

func callerBaseDir(thread *starlark.Thread) string {
	if thread == nil || thread.CallStackDepth() < 2 {
		return ""
	}
	caller := thread.CallFrame(1).Pos.Filename()
	if caller == "" || caller == "<builtin>" {
		return ""
	}
	dir := filepath.Dir(caller)
	base := filepath.Base(caller)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(dir, stem)
}
