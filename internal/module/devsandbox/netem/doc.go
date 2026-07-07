// Package netem translates link-state impairment values (delay, jitter,
// loss, bandwidth) into Linux tc/netem qdisc commands. It operates on
// an already-existing network namespace and device — it does not create
// or manage namespaces itself.
package netem
