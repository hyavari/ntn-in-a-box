package device

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestRegister(t *testing.T) {
	r := NewRegistry()

	d, err := r.Register("ue-1", TypeVirtualUE, "leo_pass_90s")
	if err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}
	if d.ID != "ue-1" {
		t.Errorf("ID = %q, want %q", d.ID, "ue-1")
	}
	if d.Type != TypeVirtualUE {
		t.Errorf("Type = %q, want %q", d.Type, TypeVirtualUE)
	}
	if d.ProfileName != "leo_pass_90s" {
		t.Errorf("ProfileName = %q, want %q", d.ProfileName, "leo_pass_90s")
	}
	if d.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestRegisterRealPhone(t *testing.T) {
	r := NewRegistry()

	d, err := r.Register("phone-1", TypeRealPhone, "geo_steady")
	if err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}
	if d.Type != TypeRealPhone {
		t.Errorf("Type = %q, want %q", d.Type, TypeRealPhone)
	}
}

func TestRegisterDuplicateID(t *testing.T) {
	r := NewRegistry()

	if _, err := r.Register("ue-1", TypeVirtualUE, "leo_pass_90s"); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	_, err := r.Register("ue-1", TypeRealPhone, "geo_steady")
	if err == nil {
		t.Fatal("expected error for duplicate ID, got nil")
	}
	if !errors.Is(err, ErrDuplicateID) {
		t.Errorf("error = %v, want ErrDuplicateID", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		name        string
		id          string
		typ         Type
		profileName string
		wantSubstr  string
	}{
		{"empty id", "", TypeVirtualUE, "leo_pass_90s", "id must not be empty"},
		{"unknown type", "ue-1", "satellite", "leo_pass_90s", "unknown type"},
		{"empty profile", "ue-1", TypeVirtualUE, "", "profile_name must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.Register(tt.id, tt.typ, tt.profileName)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !strings.Contains(got, tt.wantSubstr) {
				t.Errorf("error = %q, want substring %q", got, tt.wantSubstr)
			}
		})
	}
}

func TestGet(t *testing.T) {
	r := NewRegistry()

	if _, err := r.Register("ue-1", TypeVirtualUE, "leo_pass_90s"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	d, err := r.Get("ue-1")
	if err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	if d.ID != "ue-1" || d.Type != TypeVirtualUE || d.ProfileName != "leo_pass_90s" {
		t.Errorf("Get returned %+v, want id=ue-1 type=virtual_ue profile=leo_pass_90s", d)
	}
}

func TestGetNotFound(t *testing.T) {
	r := NewRegistry()

	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestList(t *testing.T) {
	r := NewRegistry()

	// Empty registry.
	if got := r.List(); len(got) != 0 {
		t.Errorf("List on empty registry: got %d devices, want 0", len(got))
	}

	if _, err := r.Register("ue-1", TypeVirtualUE, "leo_pass_90s"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := r.Register("phone-1", TypeRealPhone, "geo_steady"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	devices := r.List()
	if len(devices) != 2 {
		t.Fatalf("List: got %d devices, want 2", len(devices))
	}

	ids := map[string]bool{}
	for _, d := range devices {
		ids[d.ID] = true
	}
	if !ids["ue-1"] || !ids["phone-1"] {
		t.Errorf("List returned IDs %v, want ue-1 and phone-1", ids)
	}
}

func TestRemove(t *testing.T) {
	r := NewRegistry()

	if _, err := r.Register("ue-1", TypeVirtualUE, "leo_pass_90s"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := r.Remove("ue-1"); err != nil {
		t.Fatalf("Remove: unexpected error: %v", err)
	}

	// Should be gone.
	_, err := r.Get("ue-1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Remove: got %v, want ErrNotFound", err)
	}

	// List should be empty.
	if got := r.List(); len(got) != 0 {
		t.Errorf("List after Remove: got %d, want 0", len(got))
	}
}

func TestRemoveNotFound(t *testing.T) {
	r := NewRegistry()

	err := r.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestConcurrency(t *testing.T) {
	r := NewRegistry()
	const n = 100

	var wg sync.WaitGroup
	wg.Add(n)

	// Concurrent registrations with unique IDs.
	for i := range n {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("ue-%d", i)
			_, _ = r.Register(id, TypeVirtualUE, "leo_pass_90s")
		}(i)
	}
	wg.Wait()

	devices := r.List()
	if len(devices) != n {
		t.Errorf("after %d concurrent registers: got %d devices, want %d", n, len(devices), n)
	}

	// Concurrent reads + removals.
	wg.Add(n * 2)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("ue-%d", i)
			_, _ = r.Get(id)
		}(i)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("ue-%d", i)
			_ = r.Remove(id)
		}(i)
	}
	wg.Wait()
}
