package voice

// ProfileCapable reports whether a profile name is voice-oriented for
// capabilities.voice and --report voice estimates.
func ProfileCapable(name string) bool {
	switch name {
	case "lband_geo", "geo_steady", "geo_blockage":
		return true
	default:
		return false
	}
}
