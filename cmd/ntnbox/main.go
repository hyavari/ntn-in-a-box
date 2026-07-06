// Command ntnbox is the CLI entrypoint for NTN-in-a-Box.
//
// Step 0 scope: wires up the kernel (Condition Engine, event bus, device
// registry, IMS adapter, API host) behind a `ntnbox serve --profile <name>`
// command. The netns-wrapping `ntnbox run` command (Dev Sandbox, Step 1)
// is not implemented yet.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "ntnbox: not implemented yet (Step 0 in progress)")
	os.Exit(1)
}
