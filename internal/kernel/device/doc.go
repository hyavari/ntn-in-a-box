// Package device implements the device/identity registry: registering and
// looking up virtual UEs and real-phone stub devices, and associating a
// device with a profile. In-memory only for Step 0; no persistence and no
// auth/multi-tenancy yet (deferred, see design doc open questions).
package device
