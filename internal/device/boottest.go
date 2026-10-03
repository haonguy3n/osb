package device

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	osbstar "github.com/anhhao17/osb/internal/starlark"
	"golang.org/x/crypto/ssh"
)

const bootTestTimeout = 15 * time.Minute

const bootLoginMarker = "login:"

func sshHostPort(machine *osbstar.Machine) (int, error) {
	for _, p := range machine.QEMUPorts() {
		host, guest, ok := strings.Cut(p, ":")
		if !ok || guest != "22" {
			continue
		}
		n, err := strconv.Atoi(host)
		if err != nil {
			return 0, fmt.Errorf("boot-test: malformed host port %q in forward %q", host, p)
		}
		return n, nil
	}
	return 0, fmt.Errorf("boot-test: machine %q has no host forward to guest port 22 (an SSH forward like \"2222:22\" is required)", machine.Name)
}

type markerScanner struct {
	w      io.Writer
	marker []byte
	tail   []byte
	found  chan struct{}
	once   sync.Once
}

func newMarkerScanner(w io.Writer, marker string) *markerScanner {
	return &markerScanner{w: w, marker: []byte(marker), found: make(chan struct{})}
}

func (m *markerScanner) Write(p []byte) (int, error) {
	_, _ = m.w.Write(p)

	m.tail = append(m.tail, p...)
	if bytes.Contains(m.tail, m.marker) {
		m.once.Do(func() { close(m.found) })
	}
	if cap := len(m.marker) + 256; len(m.tail) > cap {
		m.tail = m.tail[len(m.tail)-cap:]
	}
	return len(p), nil
}

func runBootTest(qemuBin string, args []string, sshPort int, w io.Writer) (err error) {
	timeout := bootTestTimeout
	deadline := time.Now().Add(timeout)

	defer func() {
		if err != nil {
			fmt.Fprintln(w, "❌ Boot test: FAIL")
		}
	}()

	fmt.Fprintf(w, "🚀 Boot test: %s (timeout %s, ssh 127.0.0.1:%d)\n", qemuBin, timeout, sshPort)

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, qemuBin, args...)
	scanner := newMarkerScanner(w, bootLoginMarker)
	cmd.Stdout = scanner
	cmd.Stderr = scanner

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("boot-test: starting QEMU: %w", err)
	}

	var qemuErr error
	qemuDone := make(chan struct{})
	go func() { qemuErr = cmd.Wait(); close(qemuDone) }()

	defer func() {
		cancel()
		select {
		case <-qemuDone:
		case <-time.After(10 * time.Second):
		}
	}()

	select {
	case <-scanner.found:
		fmt.Fprintf(w, "\n🔑 Boot test: reached login prompt; connecting over SSH...\n")
	case <-qemuDone:
		return fmt.Errorf("boot-test: QEMU exited before reaching the login prompt: %w", qemuErr)
	case <-time.After(time.Until(deadline)):
		return fmt.Errorf("boot-test: timed out after %s waiting for the login prompt", timeout)
	}

	out, err := sshHealthCheck(ctx, sshPort, deadline)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "🩺 Boot test: SSH health check passed:\n%s\n", strings.TrimRight(out, "\n"))

	fmt.Fprintln(w, "✅ Boot test: PASS")
	return nil
}

func sshHealthCheck(ctx context.Context, port int, deadline time.Time) (string, error) {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	cfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.Password("")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	var client *ssh.Client
	var lastErr error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return "", fmt.Errorf("boot-test: cancelled before SSH connected")
		}
		c, err := ssh.Dial("tcp", addr, cfg)
		if err == nil {
			client = c
			break
		}
		lastErr = err
		time.Sleep(2 * time.Second)
	}
	if client == nil {
		return "", fmt.Errorf("boot-test: could not SSH to %s before timeout: %w", addr, lastErr)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("boot-test: opening SSH session: %w", err)
	}
	defer session.Close()

	const healthCmd = "uname -a"
	out, err := session.CombinedOutput(healthCmd)
	if err != nil {
		return string(out), fmt.Errorf("boot-test: health command %q failed: %w\n%s", healthCmd, err, out)
	}
	return string(out), nil
}
