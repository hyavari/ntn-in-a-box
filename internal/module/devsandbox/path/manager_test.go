package path

import "testing"

func TestInit_SelectsBearer(t *testing.T) {
	m := New(true)
	if got := m.Init(true); got != BearerSatellite {
		t.Fatalf("Init(true) = %q, want %q", got, BearerSatellite)
	}
	if tr := m.TransitionTo(BearerSatellite, false); tr != nil {
		t.Fatalf("no transition expected, got %+v", tr)
	}

	m2 := New(true)
	if got := m2.Init(false); got != BearerTerrestrial {
		t.Fatalf("Init(false) = %q, want %q", got, BearerTerrestrial)
	}
}

func TestTransitionTo_GapAndReturn(t *testing.T) {
	m := New(true)
	m.Init(true)

	tr := m.TransitionTo(Desired(false), false)
	if tr == nil {
		t.Fatal("expected transition on coverage loss")
	}
	if tr.From != BearerSatellite || tr.To != BearerTerrestrial || tr.Reason != ReasonCoverageLost {
		t.Fatalf("got %+v", tr)
	}
	if m.Current() != BearerTerrestrial {
		t.Fatalf("current = %q", m.Current())
	}

	tr = m.TransitionTo(Desired(true), false)
	if tr == nil {
		t.Fatal("expected return to satellite")
	}
	if tr.Reason != ReasonCoverageGained {
		t.Fatalf("got %+v", tr)
	}
}

func TestTransitionTo_BlockageReasons(t *testing.T) {
	m := New(true)
	m.Init(true)

	tr := m.TransitionTo(BearerTerrestrial, true)
	if tr == nil || tr.Reason != ReasonBlocked {
		t.Fatalf("want blocked, got %+v", tr)
	}
	tr = m.TransitionTo(BearerSatellite, false)
	if tr == nil || tr.Reason != ReasonUnblocked {
		t.Fatalf("want unblocked, got %+v", tr)
	}
}

func TestTransitionTo_Disabled(t *testing.T) {
	m := New(false)
	if m.Init(true) != "" {
		t.Fatal("disabled Init should return empty")
	}
	if m.TransitionTo(BearerTerrestrial, false) != nil {
		t.Fatal("disabled TransitionTo should be nil")
	}
}

func TestDesired(t *testing.T) {
	if Desired(true) != BearerSatellite || Desired(false) != BearerTerrestrial {
		t.Fatal("Desired mapping wrong")
	}
}

func TestGatewayFor(t *testing.T) {
	if got := GatewayFor(BearerSatellite, "10.200.0.1", "10.200.1.1"); got != "10.200.0.1" {
		t.Fatalf("sat gateway = %q", got)
	}
	if got := GatewayFor(BearerTerrestrial, "10.200.0.1", "10.200.1.1"); got != "10.200.1.1" {
		t.Fatalf("terr gateway = %q", got)
	}
}
