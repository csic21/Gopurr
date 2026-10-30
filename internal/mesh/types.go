// Package mesh embeds a Tailscale node in Gopurr. The official control service
// handles enrollment; local service access is denied unless explicitly enabled.
package mesh

import (
	"context"
	"net/netip"
	"time"
)

const (
	DevicesURL  = "https://login.tailscale.com/admin/machines"
	PolicyURL   = "https://login.tailscale.com/admin/acls"
	ApprovalURL = "https://login.tailscale.com/admin/settings/device-management"
)

type Device struct {
	ID, Name, DNSName, OS, Owner, Login string
	IPs                                 []netip.Addr
	Tags                                []string
	Online, Active                      bool
	LastSeen                            time.Time
	Path                                string
}

func (d Device) IP() string {
	for _, ip := range d.IPs {
		if ip.Is4() {
			return ip.String()
		}
	}
	if len(d.IPs) > 0 {
		return d.IPs[0].String()
	}
	return ""
}

type Status struct {
	Installed, Official, Running, ShieldsKnown, ShieldsUp bool
	State, Version, AuthURL, Tailnet                      string
	ProxyAddress, ProxyPassword                           string
	Services                                              []Service
	Blocked                                               []string
	Self                                                  Device
	Peers                                                 []Device
	Health                                                []string
}

type Observation struct {
	Scope  string
	Visits []Visit
}

type Visit struct {
	Key, DeviceID, Device, Owner, Login, Remote, Local string
	FirstSeen, LastSeen                                time.Time
	Active                                             bool
	Protocol                                           string
}

type Service struct {
	Protocol string
	Port     uint16
}

type Snapshot struct {
	Status                      Status
	Busy, Started, Managed      bool
	Action, Error, MonitorError string
	MonitorScope                string
	Updated                     time.Time
	Visits                      []Visit
	AutoConnect                 bool
}

// ProbeResult describes observed transport/protocol responses, never inferred
// remote authorization or successful application login.
type ProbeResult struct {
	DeviceID, State, Message string
	Port                     uint16
	Checked                  time.Time
}

type Backend interface {
	Read(context.Context) (Status, error)
	Connect(context.Context) (string, error)
	SetIncoming(context.Context, bool) error
	Disconnect(context.Context) error
	Accesses(context.Context) (Observation, error)
}
