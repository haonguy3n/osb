package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner executes one planned Step. The interface exists so tests can record
// the sequence instead of destroying a disk, and so a dry run is the same code
// path as a real one with a different Runner.
type Runner interface {
	Run(ctx context.Context, s Step) error
}

// Progress is notified before each step so a UI can show what is happening.
// n is 1-based.
type Progress func(n, total int, s Step)

// Execute runs every step in order, stopping at the first failure.
//
// There is no rollback. Once partitioning has begun the previous contents are
// gone, so unwinding would restore nothing; a failure leaves the target
// half-installed and the error says which step stopped it, which is the honest
// outcome. Re-running the installer from the start is the recovery path.
func Execute(ctx context.Context, steps []Step, r Runner, p Progress) error {
	for i, s := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p != nil {
			p(i+1, len(steps), s)
		}
		if err := r.Run(ctx, s); err != nil {
			return fmt.Errorf("step %d/%d (%s): %w", i+1, len(steps), s.Desc, err)
		}
	}
	return nil
}

// ExecRunner is the real Runner: it executes commands and writes files.
type ExecRunner struct {
	// Log receives each command line and its output. Never nil in practice;
	// a nil Log discards.
	Log io.Writer
}

func (e ExecRunner) Run(ctx context.Context, s Step) error {
	if s.IsWrite() {
		if err := os.MkdirAll(filepath.Dir(s.WritePath), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(s.Mode)
		if mode == 0 {
			mode = 0o644
		}
		e.logf("write %s (%d bytes, mode %#o)\n", s.WritePath, len(s.Content), mode)
		return os.WriteFile(s.WritePath, []byte(s.Content), mode)
	}
	if len(s.Argv) == 0 {
		return fmt.Errorf("step %q has no command", s.Desc)
	}

	// Stdin content is secret (LUKS passphrases, chpasswd lines), so the log
	// records only that stdin was supplied, never what it was.
	if s.Stdin != "" {
		e.logf("exec %s <stdin: %d bytes>\n", strings.Join(s.Argv, " "), len(s.Stdin))
	} else {
		e.logf("exec %s\n", strings.Join(s.Argv, " "))
	}

	cmd := exec.CommandContext(ctx, s.Argv[0], s.Argv[1:]...)
	if s.Stdin != "" {
		cmd.Stdin = strings.NewReader(s.Stdin)
	}
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		e.logf("%s\n", strings.TrimRight(string(out), "\n"))
	}
	if err != nil {
		return fmt.Errorf("%s: %w", s.Argv[0], err)
	}
	return nil
}

func (e ExecRunner) logf(format string, a ...any) {
	if e.Log == nil {
		return
	}
	fmt.Fprintf(e.Log, format, a...)
}

// DryRunner prints what would happen and changes nothing.
type DryRunner struct{ Out io.Writer }

func (d DryRunner) Run(_ context.Context, s Step) error {
	if s.IsWrite() {
		fmt.Fprintf(d.Out, "WRITE %s\n%s\n", s.WritePath, indent(s.Content))
		return nil
	}
	stdin := ""
	if s.Stdin != "" {
		stdin = fmt.Sprintf(" <stdin: %d bytes>", len(s.Stdin))
	}
	fmt.Fprintf(d.Out, "EXEC  %s%s\n", strings.Join(s.Argv, " "), stdin)
	return nil
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    | " + l
	}
	return strings.Join(lines, "\n")
}
