package mesh

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Shortcut struct {
	Kind     string
	Port     uint16
	User     string
	LastUsed time.Time
}

func DefaultShortcut(osName string) Shortcut {
	osName = strings.ToLower(osName)
	if strings.Contains(osName, "windows") {
		return Shortcut{Kind: "rdp", Port: 3389}
	}
	if strings.Contains(osName, "mac") || osName == "darwin" {
		return Shortcut{Kind: "vnc", Port: 5900}
	}
	return Shortcut{Kind: "ssh", Port: 22}
}

func (s Shortcut) Label() string {
	switch s.Kind {
	case "ssh":
		return "SSH"
	case "rdp":
		return "远程桌面"
	case "vnc":
		return "屏幕共享"
	}
	return "连接"
}

func TailnetIP(ip netip.Addr) bool {
	return ip.IsValid() && ip.Zone() == "" && (netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip))
}

func (s Shortcut) Validate(d Device) error {
	ip, err := netip.ParseAddr(d.IP())
	if err != nil || !TailnetIP(ip) || d.ID == "" {
		return errors.New("设备没有有效的组网地址，请刷新后重试")
	}
	if s.Port == 0 {
		return errors.New("端口须为 1–65535")
	}
	if s.Kind != "ssh" && s.Kind != "rdp" && s.Kind != "vnc" {
		return errors.New("请选择支持的连接方式")
	}
	if len(s.User) > 64 {
		return errors.New("用户名过长")
	}
	for _, r := range s.User {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return errors.New("用户名仅支持字母、数字、下划线、连字符和点；其他账号可在客户端中填写")
		}
	}
	if strings.HasPrefix(s.User, "-") {
		return errors.New("用户名不能以连字符开头")
	}
	return nil
}

func (s Shortcut) Address(d Device) string {
	return net.JoinHostPort(d.IP(), strconv.Itoa(int(s.Port)))
}

func (s Shortcut) URI(d Device) (string, error) {
	if err := s.Validate(d); err != nil {
		return "", err
	}
	if s.Kind == "rdp" {
		return "", errors.New("远程桌面使用本机连接文件打开")
	}
	u := url.URL{Scheme: s.Kind, Host: s.Address(d)}
	if s.Kind == "ssh" && s.User != "" {
		u.User = url.User(s.User)
	}
	return u.String(), nil
}

func (s Shortcut) SSHArguments(d Device) ([]string, error) {
	if err := s.Validate(d); err != nil {
		return nil, err
	}
	if s.Kind != "ssh" {
		return nil, errors.New("连接方式不是 SSH")
	}
	dst := d.IP()
	if s.User != "" {
		dst = s.User + "@" + dst
	}
	return []string{"-p", strconv.Itoa(int(s.Port)), "--", dst}, nil
}

func (s Shortcut) RDPFile(d Device) ([]byte, error) {
	if err := s.Validate(d); err != nil {
		return nil, err
	}
	if s.Kind != "rdp" {
		return nil, errors.New("连接方式不是远程桌面")
	}
	// No passwords, automatic drive sharing, or weakening of server identity
	// checks. Authentication remains the remote client's responsibility.
	return []byte(fmt.Sprintf("full address:s:%s\r\nprompt for credentials:i:1\r\nauthentication level:i:2\r\nenablecredsspsupport:i:1\r\nredirectclipboard:i:0\r\nredirectdrives:i:0\r\nredirectprinters:i:0\r\n", s.Address(d))), nil
}
