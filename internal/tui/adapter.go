package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/condition"
	"github.com/hyavari/ntn-in-a-box/internal/kernel/eventbus"
)

// Sender is the subset of tea.Program used by the Adapter. Allows
// testing without a real program.
type Sender interface {
	Send(msg tea.Msg)
}

// Adapter bridges the kernel event bus to bubbletea messages. It
// subscribes as a CoverageHandler and LinkStateHandler and forwards
// events as typed tea.Msg values via Send().
type Adapter struct {
	sender Sender
	eval   *condition.Evaluator
}

// NewAdapter creates an Adapter that sends messages to sender. The
// evaluator enriches coverage events with computed state (percentage,
// countdown).
func NewAdapter(sender Sender, eval *condition.Evaluator) *Adapter {
	return &Adapter{sender: sender, eval: eval}
}

// OnCoverage is a CoverageHandler that enriches the event with
// evaluator state and sends a CoverageMsg.
func (a *Adapter) OnCoverage(ev eventbus.CoverageEvent) {
	_, cov := a.eval.Evaluate(ev.At)
	a.sender.Send(CoverageMsg{
		Kind:                ev.Kind,
		InCoverage:          cov.InCoverage,
		ElapsedSec:          cov.ElapsedSec,
		UntilNextTransition: cov.UntilNextTransitionSec,
		At:                  ev.At,
	})
}

// OnLinkState is a LinkStateHandler that sends a LinkStateMsg.
func (a *Adapter) OnLinkState(ev eventbus.LinkStateEvent) {
	a.sender.Send(LinkStateMsg{
		State: ev.State,
		At:    ev.At,
	})
}
