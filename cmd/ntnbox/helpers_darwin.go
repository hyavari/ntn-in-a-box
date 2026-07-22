//go:build darwin

package main

import (
	"net"
	"strings"
)

// dockerHostPublishSpec returns the Docker -p publish spec for the API port.
// Loopback-oriented CLI addrs map to 127.0.0.1:port:port so messaging is not
// exposed on all host interfaces; explicit LAN binds keep port:port.
func dockerHostPublishSpec(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if !strings.Contains(addr, ":") {
			port = addr
			host = ""
		} else {
			return addrPort(addr) + ":" + addrPort(addr)
		}
	}
	if host == "" || host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return net.JoinHostPort("127.0.0.1", port) + ":" + port
	}
	return port + ":" + port
}
