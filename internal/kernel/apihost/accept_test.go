package apihost

import "testing"

func TestPrefersHTML(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		want   bool
	}{
		{"empty", "", false},
		{"json only", "application/json", false},
		{"json before html", "application/json, text/html", false},
		{"html before json", "text/html, application/json", true},
		{"html higher q", "application/json;q=0.8, text/html", true},
		{"json higher q", "text/html;q=0.5, application/json", false},
		{"browser-like", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", true},
		{"star only", "*/*", false},
		{"html present via contains trap", "application/json, text/html;q=0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prefersHTML(tt.accept); got != tt.want {
				t.Fatalf("prefersHTML(%q) = %v, want %v", tt.accept, got, tt.want)
			}
		})
	}
}
