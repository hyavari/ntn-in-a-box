package tui

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"regexp"
)

// ansiRE matches ANSI escape sequences for stripping from child output.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// CmdRunner launches a child process with its stdout/stderr captured,
// reads lines from the combined output, and sends each as an
// OutputLineMsg to the bubbletea program. On exit, it sends a
// CmdExitedMsg.
type CmdRunner struct {
	sender Sender
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// NewCmdRunner creates a CmdRunner. The command is constructed but not
// started. Call Start() to launch it.
func NewCmdRunner(sender Sender, name string, args []string) *CmdRunner {
	return &CmdRunner{
		sender: sender,
		cmd:    exec.Command(name, args...),
	}
}

// Start launches the child process and begins reading its output.
func (cr *CmdRunner) Start(ctx context.Context) error {
	ctx, cr.cancel = context.WithCancel(ctx)

	pr, pw := newPipe()
	cr.cmd.Stdout = pw
	cr.cmd.Stderr = pw
	cr.cmd.Stdin = nil // non-interactive

	if err := cr.cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return err
	}

	// Line reader goroutine.
	go func() {
		readLines(ctx, pr, cr.sender)
	}()

	// Waiter goroutine: waits for the process to exit, then sends
	// CmdExitedMsg.
	go func() {
		err := cr.cmd.Wait()
		_ = pw.Close() // unblocks the scanner
		code := cr.cmd.ProcessState.ExitCode()
		cr.sender.Send(CmdExitedMsg{Code: code, Err: err})
	}()

	return nil
}

// Signal sends a signal to the child process.
func (cr *CmdRunner) Signal(sig os.Signal) error {
	if cr.cmd.Process == nil {
		return nil
	}
	return cr.cmd.Process.Signal(sig)
}

// Cancel stops the line reader context (does not kill the process).
func (cr *CmdRunner) Cancel() {
	if cr.cancel != nil {
		cr.cancel()
	}
}

// --- shared helpers ---

// newPipe creates an io.Pipe pair.
func newPipe() (*io.PipeReader, *io.PipeWriter) {
	return io.Pipe()
}

// readLines scans lines from r and sends each as an OutputLineMsg.
// Stops when the reader is closed or ctx is cancelled.
func readLines(ctx context.Context, r io.Reader, sender Sender) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := stripANSI(scanner.Text())
		sender.Send(OutputLineMsg{Line: line})
	}
}

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}
