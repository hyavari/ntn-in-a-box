package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchMessageStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "delivered", "id": "m1"})
	}))
	defer srv.Close()

	st, err := fetchMessageStatus(srv.URL, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if st != "delivered" {
		t.Fatalf("status=%q", st)
	}
}

func TestWaitReady(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n < 2 {
			http.Error(w, "nope", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := waitReady(srv.URL+"/devices/sandbox-0/condition", 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPBaseHost(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:18091": "127.0.0.1:18091",
		":18091":          "127.0.0.1:18091",
		"18091":           "127.0.0.1:18091",
		"0.0.0.0:18091":   "127.0.0.1:18091",
	}
	for in, want := range cases {
		if got := httpBaseHost(in); got != want {
			t.Errorf("httpBaseHost(%q)=%q want %q", in, got, want)
		}
	}
}
