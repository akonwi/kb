// Package buildinfo exposes metadata injected into release builds.
package buildinfo

// Version is replaced by release builds using Go linker flags.
var Version = "dev"

// GetVersion provides an Ard-friendly function boundary.
func GetVersion() string {
	return Version
}
