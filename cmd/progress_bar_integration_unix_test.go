//go:build integration && !windows

package cmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

// TestProgressBar_RealProgram verifies the progress bar output in a real TTY program.
// It runs a helper process connected to a PTY, reads raw terminal output,
// and asserts that the rendered output includes a 100% completion state.
func TestProgressBar_RealProgram(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestPBarHelperProcess", "--")
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
	)

	// Start the process with a PTY (so TIOCGWINSZ works).
	ptmx, err := pty.Start(cmd)
	require.NoError(t, err)
	defer func() { _ = ptmx.Close() }()

	// Ensure a known terminal size
	require.NoError(t, pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80}))

	// Optional: trigger a resize mid-run to exercise SIGWINCH handling.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 40})
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGWINCH)
		}
	}()

	// Read output until the helper exits or we time out.
	var buf bytes.Buffer
	done := make(chan error, 1)

	go func() {
		// Read everything the helper writes to the PTY.
		_, _ = io.Copy(&buf, ptmx)
		// Wait for process exit (io.Copy returns when PTY closes)
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		require.NoError(t, err, "helper process should exit cleanly")
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("timeout waiting for helper process output")
	}

	out := buf.String()

	// Assertions:
	// 1) It actually rendered (not empty).
	require.NotEmpty(t, out)

	// 2) It includes a 100% state.
	// Formats:
	// - "[  0%]" / "[100%]" etc (with ANSI color codes)
	// - or "Progress: [100%]"
	//
	// Check for "100%" substring.
	require.True(t, strings.Contains(out, "100%"), "expected output to contain 100%%, got:\n%s", out)

	// 3) ANSI escape sequences (cursor save/restore etc).
	require.True(t, strings.Contains(out, "\x1B"), "expected ANSI escape sequences, got:\n%s", out)
}

// TestPBarHelperProcess is a helper "real program" executed by TestProgressBar_RealProgram.
// It MUST be in the same package and is invoked via -test.run=...
func TestPBarHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return // keep helper invisible in normal runs
	}

	pb := NewPBar()
	pb.Total = 10
	pb.SignalHandler()
	defer pb.CleanUp()

	// Render in a loop like a real program would.
	for i := 0; i <= int(pb.Total); i++ {
		pb.RenderPBar(i)
		time.Sleep(1 * time.Millisecond)
	}

	// Exit cleanly.
	os.Exit(0)
}
