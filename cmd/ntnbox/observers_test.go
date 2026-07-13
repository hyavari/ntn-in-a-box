package main

import "testing"

func TestParseObserverFlag(t *testing.T) {
	spec, err := parseObserverFlag("sandbox-0=37.7749,-122.4194")
	if err != nil {
		t.Fatal(err)
	}
	if spec.ID != "sandbox-0" || spec.LatDeg != 37.7749 || spec.LonDeg != -122.4194 {
		t.Fatalf("got %+v", spec)
	}
}

func TestParseObserverFlag_Invalid(t *testing.T) {
	cases := []string{"", "noequals", "=1,2", "id=", "id=1", "id=a,b"}
	for _, c := range cases {
		if _, err := parseObserverFlag(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestParseObserverFlags_Duplicate(t *testing.T) {
	_, err := parseObserverFlags([]string{
		"sandbox-0=1,2",
		"sandbox-0=3,4",
	})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRejectObserverDeviceMix(t *testing.T) {
	obs := []ObserverSpec{{ID: "sandbox-0", LatDeg: 1, LonDeg: 2}}
	if err := rejectObserverDeviceMix(obs, 2, 0); err == nil {
		t.Fatal("expected error for devices=2")
	}
	if err := rejectObserverDeviceMix(obs, 1, 240); err == nil {
		t.Fatal("expected error for phase-sec")
	}
	if err := rejectObserverDeviceMix(obs, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := rejectObserverDeviceMix(nil, 2, 100); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTLEObservers(t *testing.T) {
	obs, err := resolveTLEObservers(nil, 37.7, -122.4, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 1 || obs[0].ID != "sandbox-0" {
		t.Fatalf("got %+v", obs)
	}
	if _, err := resolveTLEObservers(nil, 0, 0, false); err == nil {
		t.Fatal("expected error when lat/lon unset")
	}
}
