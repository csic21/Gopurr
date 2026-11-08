package main

import (
	_ "embed"
	"encoding/json"

	"gopurr/internal/buildinfo"
)

//go:embed mygo.json
var packagedConfig []byte

func init() {
	var config struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(packagedConfig, &config); err != nil || config.Version == "" {
		panic("mygo.json must contain an application version")
	}
	// Release helpers carry an explicit linker version. Keep that value for
	// updater comparisons and daemon compatibility; plain builds use the
	// embedded config as before.
	if buildinfo.Version == "dev" {
		buildinfo.Version = config.Version
	}
}
