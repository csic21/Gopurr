// Package buildinfo shares the packaged application version with the daemon.
package buildinfo

// Version is set by release linker flags; plain application builds fall back
// to embedded mygo.json before either process starts. Other tools use dev.
var Version = "dev"
