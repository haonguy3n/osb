package device

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	osbstar "github.com/anhhao17/osb/internal/starlark"
	"golang.org/x/crypto/ssh"
)

const bootTestTimeout = 15 * time.Minute

// The login prompt appears only once the guest is up, so booting gets the whole
// budget. SSH either works shortly after that or never will - a guest whose
// network did not come up would otherwise retry until the 15 minutes are gone -
// and the smoke script gets its own deadline on the connection, so a guest that
// wedges mid-test fails instead of hanging the test forever.
const sshConnectTimeout = 3 * time.Minute

const sshTestTimeout = 5 * time.Minute

const sshDialTimeout = 5 * time.Second

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

func runBootTest(qemuBin string, args []string, sshPort int, script []byte, w io.Writer) (err error) {
	timeout := bootTestTimeout
	deadline := time.Now().Add(timeout)

	defer func() {
		if err != nil {
			fmt.Fprintln(w, "❌ Boot test: FAIL")
		}
	}()

	fmt.Fprintf(w, "🚀 Boot test: %s (boot timeout %s, ssh connect timeout %s, ssh 127.0.0.1:%d)\n",
		qemuBin, timeout, sshConnectTimeout, sshPort)

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

	if err := sshRun(ctx, sshPort, time.Now().Add(sshConnectTimeout), script, w); err != nil {
		return err
	}
	fmt.Fprintln(w, "✅ Boot test: PASS")
	return nil
}

func sshRun(ctx context.Context, port int, connectDeadline time.Time, script []byte, w io.Writer) error {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	cfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.Password("")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	var client *ssh.Client
	var lastErr error
	for time.Now().Before(connectDeadline) {
		if ctx.Err() != nil {
			return fmt.Errorf("boot-test: cancelled before SSH connected")
		}
		raw, err := net.DialTimeout("tcp", addr, sshDialTimeout)
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		// The guest answered the login prompt, so it is up: a session still
		// running when the deadline passes is a hung guest, not a slow boot.
		// The deadline lives on the connection, which bounds the script run too.
		_ = raw.SetDeadline(time.Now().Add(sshTestTimeout))
		conn, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
		if err != nil {
			raw.Close()
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		client = ssh.NewClient(conn, chans, reqs)
		break
	}
	if client == nil {
		return fmt.Errorf("boot-test: could not SSH to %s within %s: %w", addr, sshConnectTimeout, lastErr)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("boot-test: opening SSH session: %w", err)
	}
	defer session.Close()

	cmd := "uname -a"
	if len(script) > 0 {
		cmd = "sh -s"
		session.Stdin = bytes.NewReader(script)
	}
	session.Stdout = w
	session.Stderr = w
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("boot-test: %q in the guest failed: %w", cmd, err)
	}
	return nil
}
