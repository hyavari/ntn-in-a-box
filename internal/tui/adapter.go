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
	eval   condition.Eval
}

// NewAdapter creates an Adapter that sends messages to sender. The
// evaluator enriches coverage events with computed state (percentage,
// countdown).
func NewAdapter(sender Sender, eval condition.Eval) *Adapter {
	return &Adapter{sender: sender, eval: eval}
}

// OnCoverage is a CoverageHandler that enriches the event with
// evaluator state (if available) and sends a CoverageMsg.
func (a *Adapter) OnCoverage(ev eventbus.CoverageEvent) {
	msg := CoverageMsg{
		Kind:                ev.Kind,
		InCoverage:          ev.InCoverage,
		ElapsedSec:          ev.ElapsedSec,
		UntilNextTransition: ev.UntilNextTransition,
		At:                  ev.At,
	}
	// Enrich with evaluator data when available (overrides replay values).
	if a.eval != nil {
		_, cov := a.eval.Evaluate(ev.At)
		msg.InCoverage = cov.InCoverage
		msg.ElapsedSec = cov.ElapsedSec
		msg.UntilNextTransition = cov.UntilNextTransitionSec
	} else if msg.ElapsedSec == 0 && msg.UntilNextTransition == 0 {
		// Fallback: derive InCoverage from event kind.
		msg.InCoverage = ev.Kind == eventbus.KindWindowOpened || ev.Kind == eventbus.KindWindowOpening
	}
	a.sender.Send(msg)
}

// OnLinkState is a LinkStateHandler that sends a LinkStateMsg.
func (a *Adapter) OnLinkState(ev eventbus.LinkStateEvent) {
	a.sender.Send(LinkStateMsg{
		State: ev.State,
		At:    ev.At,
	})
}
