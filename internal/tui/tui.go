package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/profile"
)

// ErrReplayAgain is returned by Run when the user requests another replay.
var ErrReplayAgain = errors.New("replay again requested")

// Config holds the dependencies for running the TUI.
type Config struct {
	Bus       *eventbus.Bus
	Evaluator condition.Eval
	Profile   *profile.Profile
	CmdFn     func() *exec.Cmd // returns the prepared command (e.g. ns.Command)
	Addr      string           // API host address (for displaying GUI URL)
	IsReplay  bool             // true when running in replay mode

	// FocusDeviceID, when set, filters coverage/link bus events to that
	// device (e.g. "sandbox-0") so peer TLE observers do not thrash the panel.
	FocusDeviceID string

	// DeviceIDs is the cycle order for `d` focus switching. Length < 2 disables.
	DeviceIDs []string

	// Evals maps device id → evaluator for enrichment when focus changes.
	// Missing entries leave enrichment unset for that device.
	Evals map[string]condition.Eval

	// NotifySender is called (if non-nil) with the program's Sender
	// interface before Run blocks. This lets callers start background
	// goroutines (e.g. the replayer) that send messages into the TUI.
	// It is called synchronously before the child process starts, so
	// any goroutines launched from it are guaranteed to see a valid
	// Sender before the program begins processing.
	NotifySender func(Sender)
}

const defaultBufferCapacity = 10000

// Run starts the TUI dashboard. It blocks until the user quits or the
// context is cancelled. On exit it ensures the child is terminated
// and the terminal is restored.
func Run(ctx context.Context, cfg Config) error {
	model := NewModel(*cfg.Profile, defaultBufferCapacity)
	model.addr = cfg.Addr
	model.isReplay = cfg.IsReplay
	model.focusDeviceID = cfg.FocusDeviceID
	model.deviceIDs = append([]string(nil), cfg.DeviceIDs...)

	// Holder lets the adapter bind before tea.Program exists, then
	// forwards to the real program once constructed.
	holder := &senderHolder{}
	adapter := NewAdapter(holder, cfg.Evaluator)
	adapter.SetFocusDevice(cfg.FocusDeviceID)
	evals := cfg.Evals
	model.onFocusChange = func(id string) {
		adapter.SetFocusDevice(id)
		var eval condition.Eval
		if evals != nil {
			if e, ok := evals[id]; ok {
				adapter.SetEvaluator(e)
				eval = e
			} else {
				adapter.SetEvaluator(nil)
			}
		}
		// Refresh coverage from the newly focused evaluator so the status
		// line does not keep the previous device's in/out state.
		if eval != nil {
			now := time.Now()
			_, cov := eval.Evaluate(now)
			holder.Send(CoverageMsg{
				Kind:                "", // not a transition — no output separators
				InCoverage:          cov.InCoverage,
				ElapsedSec:          cov.ElapsedSec,
				UntilNextTransition: cov.UntilNextTransitionSec,
				At:                  now,
			})
		}
	}

	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	)
	holder.s = program

	unsubCoverage := cfg.Bus.SubscribeCoverage(adapter.OnCoverage)
	unsubLinkState := cfg.Bus.SubscribeLinkState(adapter.OnLinkState)
	unsubMessage := cfg.Bus.SubscribeMessage(adapter.OnMessage)
	defer unsubCoverage()
	defer unsubLinkState()
	defer unsubMessage()

	// Notify caller with the Sender *before* starting the child so
	// any goroutines it launches (e.g. the replayer) are guaranteed
	// to have a valid Sender when they first try to send a message.
	if cfg.NotifySender != nil {
		cfg.NotifySender(program)
	}

	// Prepare and start the child process.
	cmd := cfg.CmdFn()
	runner := &cmdRunnerCmd{sender: program, cmd: cmd}
	if err := runner.Start(ctx); err != nil {
		return err
	}

	// Run the TUI (blocks until tea.Quit).
	finalModel, err := program.Run()

	// On exit: ensure child is killed.
	_ = runner.Signal(syscall.SIGTERM)
	select {
	case <-runner.done:
	case <-time.After(3 * time.Second):
		_ = runner.Signal(os.Kill)
		<-runner.done
	}

	if err != nil {
		return err
	}

	// Check if the user requested replay-again.
	if m, ok := finalModel.(Model); ok && m.replayAgain {
		return ErrReplayAgain
	}

	return nil
}

// senderHolder implements Sender and forwards once the program is set.
type senderHolder struct {
	s Sender
}

func (h *senderHolder) Send(msg tea.Msg) {
	if h.s != nil {
		h.s.Send(msg)
	}
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
