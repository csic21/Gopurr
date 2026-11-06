// Package remote serves the authenticated remote control console. It is
// deliberately separate from the kernel controller: the browser never sees the
// mihomo socket, its secret, or a raw controller route.
package remote

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopurr/internal/securefs"
)

const (
	// DefaultBind limits new installations to this device. The built-in
	// mesh can forward localhost; LAN access requires an explicit selection.
	DefaultBind = "127.0.0.1"
	DefaultPort = 7899
	TokenBytes  = 32
)

// Config is the persistent remote console configuration. The token is a local
// credential and is written with the same private-file rules as the kernel
// secret.
type Config struct {
	Enabled bool   `json:"enabled"`
	Bind    string `json:"bind"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
}

func DefaultConfig() Config {
	return Config{Bind: DefaultBind, Port: DefaultPort}
}

// LoadConfig returns defaults when the file does not exist yet. A malformed
// file returns defaults together with the read error so the caller can explain
// why remote access stayed off without losing the rest of the app.
func LoadConfig(path string) (Config, error) {
	if path == "" {
		return DefaultConfig(), nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return DefaultConfig(), err
	}
	if err := securefs.PrivateFile(path); err != nil {
		return DefaultConfig(), err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(b, &cfg); err != nil {
		return DefaultConfig(), fmt.Errorf("invalid remote config: %w", err)
	}
	return cfg.Normalize(), nil
}

// Normalize fills defaults without inventing a token. Token creation is a
// separate, explicit step so a disabled console does not write credentials.
func (c Config) Normalize() Config {
	c.Bind = strings.TrimSpace(c.Bind)
	if c.Bind == "" {
		c.Bind = DefaultBind
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	c.Token = strings.TrimSpace(c.Token)
	return c
}

func (c Config) Validate() error {
	if err := ValidateBind(c.Bind); err != nil {
		return err
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func ValidateBind(bind string) error {
	bind = strings.TrimSpace(bind)
	if bind == "" {
		return fmt.Errorf("listen address is required")
	}
	if bind == "localhost" {
		return nil
	}
	if net.ParseIP(bind) == nil {
		return fmt.Errorf("listen address must be an IP address")
	}
	return nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.Bind, strconv.Itoa(c.Port))
}

// EnsureToken creates the bearer token on first enable. Existing tokens are
// preserved so paired browsers keep working across restarts.
func (c *Config) EnsureToken() error {
	if strings.TrimSpace(c.Token) != "" {
		return nil
	}
	token, err := GenerateToken()
	if err != nil {
		return err
	}
	c.Token = token
	return nil
}

func GenerateToken() (string, error) {
	buf := make([]byte, TokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (c Config) Save(path string) error {
	if path == "" {
		return errors.New("remote config path is empty")
	}
	if err := securefs.PrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c.Normalize(), "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return securefs.WriteFile(path, b, 0o600)
}

// AccessURLs lists the obvious browser entry points. It never guesses a public
// address: a public IP depends on the router, and showing a wrong one is worse
// than asking the user to use the address they forwarded.
func AccessURLs(c Config) []string {
	c = c.Normalize()
	port := strconv.Itoa(c.Port)
	seen := map[string]bool{}
	var out []string
	add := func(host string) {
		if host == "" {
			return
		}
		u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}
		if seen[u.String()] {
			return
		}
		seen[u.String()] = true
		out = append(out, u.String())
	}

	if ip := net.ParseIP(c.Bind); ip != nil && !ip.IsUnspecified() {
		add(c.Bind)
		if ip.IsLoopback() {
			return out
		}
	}
	if c.Bind == "localhost" {
		add("127.0.0.1")
		return out
	}
	add("127.0.0.1")
	if ip := net.ParseIP(c.Bind); ip != nil && !ip.IsUnspecified() {
		return out
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || !ip.IsGlobalUnicast() {
			continue
		}
		add(ip.String())
	}
	return out
}
