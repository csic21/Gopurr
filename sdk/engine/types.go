// Package engine is Gopurr's host-independent application boundary. It contains
// no UI framework, OS proxy mutation, privileged service, or mobile VPN host.
// Each process owns at most one engine because mihomo itself uses singletons.
package engine

import (
	"encoding/json"
	"time"
)

const APIVersion = 1
const MaxDocumentBytes = 16 << 20

type Options struct {
	// Home is required and absolute. Hosts must provide their own application
	// sandbox, separate from the existing desktop application's data directory.
	Home string `json:"home"`
	// MixedPort is a loopback-only HTTP/SOCKS port. Zero picks an available port.
	MixedPort int `json:"mixedPort,omitempty"`
}

type Request struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error codes are stable protocol tokens; messages may be displayed to users.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

type Capabilities struct {
	SystemProxy bool `json:"systemProxy"`
	Tun         bool `json:"tun"`
}

type Traffic struct {
	Up          int64  `json:"up"`
	Down        int64  `json:"down"`
	UpTotal     int64  `json:"upTotal"`
	DownTotal   int64  `json:"downTotal"`
	Connections int    `json:"connections"`
	Memory      uint64 `json:"memory"`
	At          int64  `json:"at"`
}

type Status struct {
	APIVersion        int          `json:"apiVersion"`
	Running           bool         `json:"running"`
	ActiveProfile     string       `json:"activeProfile"`
	MixedPort         int          `json:"mixedPort"`
	Mode              string       `json:"mode"`
	SystemProxy       bool         `json:"systemProxy"`
	Tun               bool         `json:"tun"`
	VPNSupported      bool         `json:"vpnSupported"`
	Capabilities      Capabilities `json:"capabilities"`
	Traffic           Traffic      `json:"traffic"`
	Error             string       `json:"error,omitempty"`
	Busy              bool         `json:"busy"`
	Operation         *Operation   `json:"operation,omitempty"`
	ReconnectRequired bool         `json:"reconnectRequired"`
	TunnelPlanVersion int          `json:"tunnelPlanVersion"`
	AppScope          AppScope     `json:"appScope"`
}

type Operation struct {
	Method    string `json:"method"`
	ProfileID string `json:"profileId,omitempty"`
	Phase     string `json:"phase"`
}

type Profile struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Kind              string    `json:"kind"`
	Source            string    `json:"source"`
	Updated           time.Time `json:"updated"`
	Active            bool      `json:"active"`
	ReconnectRequired bool      `json:"reconnectRequired"`
}

type ProfileMutation struct {
	Profile           *Profile `json:"profile,omitempty"`
	Saved             bool     `json:"saved"`
	Applied           bool     `json:"applied"`
	ReconnectRequired bool     `json:"reconnectRequired"`
	Status            Status   `json:"status"`
}

type ProfileEditor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	Kind string `json:"kind"`
}

type ProfileList struct {
	Active   string    `json:"active"`
	Profiles []Profile `json:"profiles"`
}

type Member struct {
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Delay *uint16 `json:"delay,omitempty"`
}

type Group struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now"`
	Hidden  bool     `json:"hidden"`
	Members []Member `json:"members"`
}

type GroupList struct {
	Groups []Group `json:"groups"`
}

type Log struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}
