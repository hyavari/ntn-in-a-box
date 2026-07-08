package tui

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// Config holds the dependencies for running the TUI.
type Config struct {
	Bus       *eventbus.Bus
	Evaluator *condition.Evaluator
	Profile   *profile.Profile
	CmdFn     func() *exec.Cmd // returns the prepared command (e.g. ns.Command)
	Addr      string           // API host address (for displaying GUI URL)
}

const defaultBufferCapacity = 10000

// Run starts the TUI dashboard. It blocks until the user quits or the
// child process exits and the user dismisses the TUI. On exit it
// ensures the child is terminated and the terminal is restored.
func Run(ctx context.Context, cfg Config) error {
	model := NewModel(*cfg.Profile, defaultBufferCapacity)
	model.addr = cfg.Addr

	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	)

	// Set up the adapter and subscribe to the bus.
	adapter := NewAdapter(program, cfg.Evaluator)
	cfg.Bus.SubscribeCoverage(adapter.OnCoverage)
	cfg.Bus.SubscribeLinkState(adapter.OnLinkState)

	// Prepare and start the child process.
	cmd := cfg.CmdFn()
	runner := &cmdRunnerCmd{sender: program, cmd: cmd}
	if err := runner.Start(ctx); err != nil {
		return err
	}

	// Run the TUI (blocks until tea.Quit).
	_, err := program.Run()

	// On exit: ensure child is killed.
	_ = runner.Signal(syscall.SIGTERM)
	select {
	case <-runner.done:
	case <-time.After(3 * time.Second):
		_ = runner.Signal(os.Kill)
		<-runner.done
	}

	return err
}

// cmdRunnerCmd is like CmdRunner but takes a pre-built *exec.Cmd
// (used when the command is already constructed via ns.Command).
type cmdRunnerCmd struct {
	sender Sender
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{} // closed when cmd.Wait() completes
}

func (cr *cmdRunnerCmd) Start(ctx context.Context) error {
	ctx, cr.cancel = context.WithCancel(ctx)
	cr.done = make(chan struct{})

	pr, pw := newPipe()
	cr.cmd.Stdout = pw
	cr.cmd.Stderr = pw
	cr.cmd.Stdin = nil

	if err := cr.cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return err
	}

	go func() {
		readLines(ctx, pr, cr.sender)
	}()

	go func() {
		err := cr.cmd.Wait()
		pw.Close()
		code := cr.cmd.ProcessState.ExitCode()
		cr.sender.Send(CmdExitedMsg{Code: code, Err: err})
		close(cr.done)
	}()

	return nil
}

func (cr *cmdRunnerCmd) Signal(sig os.Signal) error {
	if cr.cmd.Process == nil {
		return nil
	}
	return cr.cmd.Process.Signal(sig)
}
