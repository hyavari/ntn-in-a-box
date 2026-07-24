package main

// version is overwritten at release build time via:
//
//	-ldflags "-X main.version=vX.Y.Z"
//
// Local / CI builds keep "dev".
var version = "dev"
