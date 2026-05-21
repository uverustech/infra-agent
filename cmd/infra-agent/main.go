package main

// version is set by scripts/bump-version.go on each commit and overridden at
// release build time via -ldflags "-X main.version=vX.Y.Z".
var version = "v1.11.7"

func main() {
	Execute()
}
