// Package buildinfo shares the packaged application version with the daemon.
package buildinfo

// Version is read from the embedded mygo.json by main before either process
// starts. Tests and unbundled tools use dev.
var Version = "dev"
