package remote

import (
	"context"
	"time"
)

// Status is the small snapshot the console needs on every poll. Counts are
// intentionally read from the kernel cache rather than recomputed per request.
type Status struct {
	Version              string  `json:"version"`
	Kernel               bool    `json:"kernel"`
	Mode                 string  `json:"mode"`
	SystemProxy          bool    `json:"systemProxy"`
	SystemProxyAvailable bool    `json:"systemProxyAvailable"`
	Tun                  bool    `json:"tun"`
	TunAvailable         bool    `json:"tunAvailable"`
	TunInstalled         bool    `json:"tunInstalled"`
	Busy                 bool    `json:"busy"`
	Traffic              Traffic `json:"traffic"`
	Connections          int     `json:"connections"`
	ConnectionsStale     bool    `json:"connectionsStale"`
	Rules                int     `json:"rules"`
	Proxies              int     `json:"proxies"`
	Notice               string  `json:"notice,omitempty"`
	Error                string  `json:"error,omitempty"`
}

type Traffic struct {
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
	At        int64 `json:"at"` // unix milliseconds, 0 when no sample
	Stale     bool  `json:"stale"`
}

type Group struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now"`
	Hidden  bool     `json:"hidden"`
	Global  bool     `json:"global"`
	Members []Member `json:"members"`
}

type Member struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Delay      *int   `json:"delay"`
	DelayKnown bool   `json:"delayKnown"`
}

type Connection struct {
	ID              string    `json:"id"`
	Host            string    `json:"host"`
	Network         string    `json:"network"`
	Process         string    `json:"process"`
	ProcessPath     string    `json:"processPath"`
	Source          string    `json:"source"`
	Destination     string    `json:"destination"`
	Rule            string    `json:"rule"`
	RulePayload     string    `json:"rulePayload"`
	Chain           string    `json:"chain"`
	Chains          []string  `json:"chains"`
	Up              int64     `json:"up"`
	Down            int64     `json:"down"`
	Start           time.Time `json:"start"`
	SourceIP        string    `json:"sourceIP,omitempty"`
	SourcePort      string    `json:"sourcePort,omitempty"`
	DestinationIP   string    `json:"destinationIP,omitempty"`
	DestinationPort string    `json:"destinationPort,omitempty"`
}

type LogEntry struct {
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

type DelayResult struct {
	Delay int    `json:"delay"`
	Error string `json:"error,omitempty"`
}

// Host is implemented by the desktop app. The web server never imports the
// app package, so it cannot reach UI fields or the kernel's local socket by
// accident.
type Host interface {
	Status(ctx context.Context) (Status, error)
	Groups(ctx context.Context) ([]Group, error)
	Select(ctx context.Context, group, member string) error
	SetMode(ctx context.Context, mode string) error
	SetSystemProxy(ctx context.Context, on bool) error
	SetTun(ctx context.Context, on bool) error
	Connections(ctx context.Context) ([]Connection, error)
	CloseConnection(ctx context.Context, id string) error
	CloseAllConnections(ctx context.Context) error
	Logs(ctx context.Context) ([]LogEntry, error)
	ClearLogs(ctx context.Context) error
	Delay(ctx context.Context, group string, names []string) (map[string]DelayResult, error)
	Language() string
}

func validMode(mode string) bool {
	switch mode {
	case "rule", "global", "direct":
		return true
	}
	return false
}
