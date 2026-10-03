package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type Execer interface {
	Run(ctx context.Context, cfg *SandboxConfig, command string, privileged bool) (ExecResult, error)
	RunHost(ctx context.Context, command string, dir string) (ExecResult, error)
}

type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type RealExecer struct{}

func (RealExecer) Run(ctx context.Context, cfg *SandboxConfig, command string, privileged bool) (ExecResult, error) {
	cfg.Ctx = ctx

	var stdoutBuf, stderrBuf bytes.Buffer
	origStdout, origStderr := cfg.Stdout, cfg.Stderr
	if origStdout != nil {
		cfg.Stdout = io.MultiWriter(origStdout, &stdoutBuf)
	} else {
		cfg.Stdout = &stdoutBuf
	}
	if origStderr != nil {
		cfg.Stderr = io.MultiWriter(origStderr, &stderrBuf)
	} else {
		cfg.Stderr = &stderrBuf
	}

	var err error
	if privileged {
		cfg.NoUser = true
		err = RunSimple(cfg, command)
		cfg.NoUser = false
	} else {
		err = RunInSandbox(cfg, command)
	}

	cfg.Stdout, cfg.Stderr = origStdout, origStderr

	if err != nil {
		return ExecResult{ExitCode: 1, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}, err
	}
	return ExecResult{ExitCode: 0, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}, nil
}

func (RealExecer) RunHost(ctx context.Context, command string, dir string) (ExecResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	return ExecResult{
		ExitCode: exitCode,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
	}, err
}

const sandboxKey = "osb.sandbox"
const execerKey = "osb.execer"
const contextKey = "osb.context"

func NewBuildThread(ctx context.Context, cfg *SandboxConfig, execer Execer) *starlark.Thread {
	t := &starlark.Thread{Name: "build"}
	t.SetLocal(sandboxKey, cfg)
	t.SetLocal(execerKey, execer)
	t.SetLocal(contextKey, ctx)
	t.SetLocal("osb.run", starlark.NewBuiltin("run", fnRun))
	t.SetLocal("osb.install_uki", starlark.NewBuiltin("install_uki", fnInstallUKI))
	return t
}

func fnRun(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var command starlark.String
	if err := starlark.UnpackPositionalArgs("run", args, nil, 1, &command); err != nil {
		return nil, err
	}

	check := true
	privileged := false
	host := false
	for _, kv := range kwargs {
		key := string(kv[0].(starlark.String))
		if key == "check" {
			if b, ok := kv[1].(starlark.Bool); ok {
				check = bool(b)
			}
		}
		if key == "privileged" {
			if b, ok := kv[1].(starlark.Bool); ok {
				privileged = bool(b)
			}
		}
		if key == "host" {
			if b, ok := kv[1].(starlark.Bool); ok {
				host = bool(b)
			}
		}
	}

	if host {
		ctx := thread.Local(contextKey).(context.Context)
		execer := thread.Local(execerKey).(Execer)
		cfg := thread.Local(sandboxKey).(*SandboxConfig)
		result, err := execer.RunHost(ctx, string(command), cfg.HostDir)

		resultStruct := starlarkstruct.FromStringDict(starlark.String("result"), starlark.StringDict{
			"exit_code": starlark.MakeInt(result.ExitCode),
			"stdout":    starlark.String(result.Stdout),
			"stderr":    starlark.String(result.Stderr),
		})

		if err != nil && check {
			return nil, fmt.Errorf("run(%q, host=True) failed: exit code %d\n%s",
				string(command), result.ExitCode, result.Stderr)
		}

		return resultStruct, nil
	}

	cfg := thread.Local(sandboxKey).(*SandboxConfig)
	execer := thread.Local(execerKey).(Execer)
	ctx := thread.Local(contextKey).(context.Context)

	result, err := execer.Run(ctx, cfg, string(command), privileged)

	resultStruct := starlarkstruct.FromStringDict(starlark.String("result"), starlark.StringDict{
		"exit_code": starlark.MakeInt(result.ExitCode),
		"stdout":    starlark.String(result.Stdout),
		"stderr":    starlark.String(result.Stderr),
	})

	if err != nil && check {
		return nil, fmt.Errorf("run(%s) failed: exit code %d\n%s",
			shortCommand(string(command)), result.ExitCode, result.Stderr)
	}

	return resultStruct, nil
}

func shortCommand(c string) string {
	c = strings.TrimSpace(c)
	if i := strings.IndexByte(c, '\n'); i >= 0 {
		c = c[:i] + " ..."
	}
	if len(c) > 120 {
		c = c[:120] + "..."
	}
	return fmt.Sprintf("%q", c)
}
