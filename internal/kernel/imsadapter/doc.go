// Package imsadapter defines the IMSAdapter interface (submit message,
// receive delivery/read receipts) and its Step 0 mock/loopback
// implementation: queued -> in-flight -> delivered/failed transitions,
// simulated receipts with timestamps, and configurable failure injection.
// A real-backend implementation is added in Step 3.
package imsadapter
