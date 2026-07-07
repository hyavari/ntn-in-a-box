package device

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Type distinguishes the two device kinds the kernel tracks.
type Type string

const (
	// TypeVirtualUE is a software-simulated device — no real hardware.
	TypeVirtualUE Type = "virtual_ue"

	// TypeRealPhone is a stub representing a real phone (VoWiFi UE).
	// The kernel doesn't interact with the phone directly in Step 0;
	// this type exists so the registry can distinguish the two once
	// the IMS Adapter (Task 8) and real-backend swap (Step 3) land.
	TypeRealPhone Type = "real_phone"
)

// Device is a registered device in the kernel's identity registry.
type Device struct {
	ID          string
	Type        Type
	ProfileName string
	CreatedAt   time.Time
}

// Registry is the kernel's in-memory device/identity registry.
// Safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	devices map[string]Device
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{devices: make(map[string]Device)}
}

// Register adds a new device. Returns ErrDuplicateID if a device with
// the same ID already exists.
func (r *Registry) Register(id string, typ Type, profileName string) (Device, error) {
	if id == "" {
		return Device{}, errors.New("device: id must not be empty")
	}
	if typ != TypeVirtualUE && typ != TypeRealPhone {
		return Device{}, fmt.Errorf("device: unknown type %q", typ)
	}
	if profileName == "" {
		return Device{}, errors.New("device: profile_name must not be empty")
	}

	d := Device{
		ID:          id,
		Type:        typ,
		ProfileName: profileName,
		CreatedAt:   time.Now(),
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.devices[id]; exists {
		return Device{}, fmt.Errorf("%w: %s", ErrDuplicateID, id)
	}
	r.devices[id] = d
	return d, nil
}

// Get returns the device with the given ID, or ErrNotFound.
func (r *Registry) Get(id string) (Device, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	d, ok := r.devices[id]
	if !ok {
		return Device{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return d, nil
}

// List returns all registered devices in no guaranteed order.
func (r *Registry) List() []Device {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Device, 0, len(r.devices))
	for _, d := range r.devices {
		out = append(out, d)
	}
	return out
}

// Remove deletes the device with the given ID. Returns ErrNotFound if
// no such device exists.
func (r *Registry) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.devices[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	delete(r.devices, id)
	return nil
}

// Sentinel errors for the device registry.
var (
	ErrDuplicateID = errors.New("device: duplicate id")
	ErrNotFound    = errors.New("device: not found")
)
