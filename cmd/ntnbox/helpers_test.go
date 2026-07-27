package main

import (
	"reflect"
	"testing"
)

func TestPlanSandboxListen_DualBindsLoopback(t *testing.T) {
	got := planSandboxListen("127.0.0.1:8080")
	wantAddrs := []string{"127.0.0.1:8080", "10.200.0.1:8080"}
	if !reflect.DeepEqual(got.Addrs, wantAddrs) {
		t.Fatalf("Addrs = %v, want %v", got.Addrs, wantAddrs)
	}
	if got.APIBase != "http://10.200.0.1:8080" {
		t.Fatalf("APIBase = %q, want http://10.200.0.1:8080", got.APIBase)
	}
}

func TestPlanSandboxListen_IPv6Loopback(t *testing.T) {
	got := planSandboxListen("[::1]:8080")
	wantAddrs := []string{"[::1]:8080", "10.200.0.1:8080"}
	if !reflect.DeepEqual(got.Addrs, wantAddrs) {
		t.Fatalf("Addrs = %v, want %v", got.Addrs, wantAddrs)
	}
	if got.APIBase != "http://10.200.0.1:8080" {
		t.Fatalf("APIBase = %q, want http://10.200.0.1:8080", got.APIBase)
	}
}

func TestPlanSandboxListen_Localhost(t *testing.T) {
	got := planSandboxListen("localhost:8080")
	wantAddrs := []string{"127.0.0.1:8080", "[::1]:8080", "10.200.0.1:8080"}
	if !reflect.DeepEqual(got.Addrs, wantAddrs) {
		t.Fatalf("Addrs = %v, want %v", got.Addrs, wantAddrs)
	}
	if got.APIBase != "http://10.200.0.1:8080" {
		t.Fatalf("APIBase = %q, want http://10.200.0.1:8080", got.APIBase)
	}
}

func TestPlanSandboxListen_BarePort(t *testing.T) {
	got := planSandboxListen(":9090")
	wantAddrs := []string{"127.0.0.1:9090", "10.200.0.1:9090"}
	if !reflect.DeepEqual(got.Addrs, wantAddrs) {
		t.Fatalf("Addrs = %v, want %v", got.Addrs, wantAddrs)
	}
	if got.APIBase != "http://10.200.0.1:9090" {
		t.Fatalf("APIBase = %q, want http://10.200.0.1:9090", got.APIBase)
	}
}

func TestPlanSandboxListen_AllInterfaces(t *testing.T) {
	got := planSandboxListen("0.0.0.0:8080")
	if !reflect.DeepEqual(got.Addrs, []string{"0.0.0.0:8080"}) {
		t.Fatalf("Addrs = %v, want [0.0.0.0:8080]", got.Addrs)
	}
	if got.APIBase != "http://10.200.0.1:8080" {
		t.Fatalf("APIBase = %q, want http://10.200.0.1:8080", got.APIBase)
	}
}

func TestPlanSandboxListen_SpecificHost(t *testing.T) {
	got := planSandboxListen("192.168.1.5:8080")
	wantAddrs := []string{"192.168.1.5:8080", "10.200.0.1:8080"}
	if !reflect.DeepEqual(got.Addrs, wantAddrs) {
		t.Fatalf("Addrs = %v, want %v", got.Addrs, wantAddrs)
	}
	if got.APIBase != "http://10.200.0.1:8080" {
		t.Fatalf("APIBase = %q, want http://10.200.0.1:8080", got.APIBase)
	}
}

func TestPlanSandboxListen_GatewayOnly(t *testing.T) {
	got := planSandboxListen("10.200.0.1:8080")
	if !reflect.DeepEqual(got.Addrs, []string{"10.200.0.1:8080"}) {
		t.Fatalf("Addrs = %v, want [10.200.0.1:8080]", got.Addrs)
	}
	if got.APIBase != "http://10.200.0.1:8080" {
		t.Fatalf("APIBase = %q, want http://10.200.0.1:8080", got.APIBase)
	}
}

func TestSandboxBaseCovered(t *testing.T) {
	tests := []struct {
		name      string
		want      string
		listeners []string
		ok        bool
	}{
		{"exact gateway", "10.200.0.1:8080", []string{"127.0.0.1:8080", "10.200.0.1:8080"}, true},
		{"wildcard ipv4", "10.200.0.1:8080", []string{"0.0.0.0:8080"}, true},
		{"wildcard ipv6", "10.200.0.1:8080", []string{"[::]:8080"}, true},
		{"wrong port", "10.200.0.1:8080", []string{"0.0.0.0:9090"}, false},
		{"loopback only", "10.200.0.1:8080", []string{"127.0.0.1:8080"}, false},
		{"specific other host", "10.200.0.1:8080", []string{"192.168.1.5:8080"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sandboxBaseCovered(tt.want, tt.listeners); got != tt.ok {
				t.Fatalf("sandboxBaseCovered(%q, %v) = %v, want %v", tt.want, tt.listeners, got, tt.ok)
			}
		})
	}
}
