package mesh

import "gopurr/internal/localize"

import (
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"
)

func ValidAuthURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "login.tailscale.com" && u.User == nil && u.Fragment == "" && strings.HasPrefix(u.Path, "/")
}

type wirePeer struct {
	ID, HostName, DNSName, OS string
	UserID                    int64
	TailscaleIPs              []netip.Addr
	Tags                      []string
	Online, Active            bool
	LastSeen                  time.Time
	CurAddr, Relay, PeerRelay string
}
type wireUser struct{ DisplayName, LoginName string }
type wireStatus struct {
	BackendState, Version, AuthURL string
	Self                           *wirePeer
	Peer                           map[string]*wirePeer
	User                           map[int64]wireUser
	CurrentTailnet                 *struct{ Name string }
	Health                         []string
}

func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	r := []rune(s)
	if len(r) > 256 {
		s = string(r[:256])
	}
	return strings.TrimSpace(s)
}

func device(p *wirePeer, users map[int64]wireUser) Device {
	if p == nil {
		return Device{}
	}
	u := users[p.UserID]
	d := Device{ID: p.ID, Name: clean(p.HostName), DNSName: clean(strings.TrimSuffix(p.DNSName, ".")), OS: clean(p.OS),
		Owner: clean(u.DisplayName), Login: clean(u.LoginName), IPs: p.TailscaleIPs, Tags: p.Tags, Online: p.Online, Active: p.Active, LastSeen: p.LastSeen}
	if d.Name == "" {
		d.Name = d.DNSName
	}
	if d.Name == "" {
		d.Name = d.IP()
	}
	if len(d.Tags) > 0 {
		d.Owner, d.Login = localize.Text("带标签的设备"), ""
	}
	if p.Active {
		switch {
		case p.CurAddr != "":
			d.Path = localize.Text("直连")
		case p.PeerRelay != "":
			d.Path = localize.Text("设备中继")
		case p.Relay != "":
			d.Path = localize.Text("加密中继")
		}
	}
	return d
}
