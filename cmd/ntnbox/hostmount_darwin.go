//go:build darwin

package main

// appendDarwinMountArgs adds -v flags for mounts and optional pnpm store volume.
func appendDarwinMountArgs(dockerArgs []string, mounts []hostMount) []string {
	for _, m := range mounts {
		dockerArgs = append(dockerArgs, "-v", m.HostPath+":"+m.ContainerPath)
		if m.NodeModulesVolume != "" {
			dockerArgs = append(dockerArgs,
				"-v", m.NodeModulesVolume+":"+m.ContainerPath+"/node_modules",
			)
		}
	}
	hasJS := false
	for _, m := range mounts {
		if m.NodeModulesVolume != "" {
			hasJS = true
			break
		}
	}
	if hasJS {
		dockerArgs = append(dockerArgs, "-v", "ntnbox-pnpm-store:/root/.local/share/pnpm")
	}
	return dockerArgs
}
