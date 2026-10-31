// Package kernel is the client side of the app: the kernel runs in its own
// process (root when TUN is on), and this package speaks the standard mihomo
// controller protocol to it over a unix socket.
//
// No private protocol, no fd passing, no route code of our own: the daemon is
// mihomo, and the wire format is the one every Clash dashboard already speaks.
package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/localize"
)

// Remote is an HTTP client bound to a unix socket.
type Remote struct {
	socket string
	secret string

	stream *http.Client // no timeout: traffic and logs are long-lived
	call   *http.Client // short timeout for request/response

	mu       sync.Mutex
	logs     []core.LogLine
	traffic  core.Sample
	conns    int
	lastErr  string
	quiet    atomic.Bool
	pollWake chan struct{}
}

func NewRemote(socket, secret string) *Remote {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}
	return &Remote{
		socket:   socket,
		secret:   secret,
		pollWake: make(chan struct{}, 1),
		stream: &http.Client{Transport: &http.Transport{
			DialContext:           dial,
			MaxIdleConns:          4,
			IdleConnTimeout:       0,
			ResponseHeaderTimeout: 5 * time.Second,
		}},
		call: &http.Client{
			Transport: &http.Transport{DialContext: dial},
			Timeout:   6 * time.Second,
		},
	}
}

func (r *Remote) request(method, path string, body io.Reader, out any) error {
	return r.requestContext(context.Background(), r.call, method, path, body, out)
}

func (r *Remote) requestContext(ctx context.Context, client *http.Client, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, body)
	if err != nil {
		return err
	}
	if r.secret != "" {
		req.Header.Set("Authorization", "Bearer "+r.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: HTTP %d %s", method, path, resp.StatusCode,
			strings.TrimSpace(string(msg)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Probe reports whether a daemon answers on the socket.
func (r *Remote) Probe(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/version", nil)
	if err != nil {
		return err
	}
	if r.secret != "" {
		req.Header.Set("Authorization", "Bearer "+r.secret)
	}
	resp, err := r.call.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("controller answered HTTP %d", resp.StatusCode)
	}
	return nil
}

// resetTransport drops idle connections so a restarted daemon at the same
// socket path is not reached through a dead keep-alive to the previous process.
func (r *Remote) resetTransport() {
	if r == nil {
		return
	}
	if t, ok := r.stream.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
	if t, ok := r.call.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}

// ---------- streams ----------

// Watch starts the traffic and log streams. Safe to call once.
func (r *Remote) Watch() {
	go r.watchTraffic()
	go r.watchLogs()
	go r.pollConnections()
}

func (r *Remote) streamGet(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, "http://localhost"+path, nil)
	if err != nil {
		return nil, err
	}
	if r.secret != "" {
		req.Header.Set("Authorization", "Bearer "+r.secret)
	}
	return r.stream.Do(req)
}

func (r *Remote) watchTraffic() {
	for {
		resp, err := r.streamGet("/traffic")
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1<<20)
		for scanner.Scan() {
			var t struct {
				Up        int64 `json:"up"`
				Down      int64 `json:"down"`
				UpTotal   int64 `json:"upTotal"`
				DownTotal int64 `json:"downTotal"`
			}
			if json.Unmarshal(scanner.Bytes(), &t) != nil {
				continue
			}
			r.mu.Lock()
			r.traffic.UpNow = t.Up
			r.traffic.DownNow = t.Down
			r.traffic.UpTotal = t.UpTotal
			r.traffic.DownTotal = t.DownTotal
			r.mu.Unlock()
		}
		resp.Body.Close()
		time.Sleep(time.Second)
	}
}

func (r *Remote) watchLogs() {
	for {
		resp, err := r.streamGet("/logs?level=info")
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1<<20)
		for scanner.Scan() {
			var ev struct {
				Type    string `json:"type"`
				Payload string `json:"payload"`
			}
			if json.Unmarshal(scanner.Bytes(), &ev) != nil || ev.Payload == "" {
				continue
			}
			r.mu.Lock()
			r.logs = append(r.logs, core.LogLine{At: time.Now(), Level: ev.Type, Text: ev.Payload})
			if len(r.logs) > 400 {
				r.logs = r.logs[len(r.logs)-400:]
			}
			r.mu.Unlock()
		}
		resp.Body.Close()
		time.Sleep(time.Second)
	}
}

// pollConnections keeps a live count without hammering the controller.
func (r *Remote) pollConnections() {
	for {
		var out struct {
			Connections []json.RawMessage `json:"connections"`
		}
		if err := r.request(http.MethodGet, "/connections", nil, &out); err == nil {
			r.mu.Lock()
			r.conns = len(out.Connections)
			r.mu.Unlock()
		}
		timer := time.NewTimer(r.connectionPollInterval())
		select {
		case <-timer.C:
		case <-r.pollWake:
		}
		timer.Stop()
	}
}

func (r *Remote) connectionPollInterval() time.Duration {
	if r.quiet.Load() {
		return 15 * time.Second
	}
	return 2 * time.Second
}

func (r *Remote) SetUIActive(active bool) {
	r.quiet.Store(!active)
	select {
	case r.pollWake <- struct{}{}:
	default:
	}
}

// ---------- reads ----------

func (r *Remote) Sample() core.Sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.traffic
	s.Conns = r.conns
	return s
}

func (r *Remote) Connections(limit int) []core.Conn {
	conns, err := r.ConnectionsWithError()
	if err != nil {
		r.setErr(err)
		return nil
	}
	if limit > 0 && len(conns) > limit {
		return conns[:limit]
	}
	return conns
}

func (r *Remote) ConnectionsWithError() ([]core.Conn, error) {
	var out struct {
		Connections []struct {
			ID       string `json:"id"`
			Metadata struct {
				Host        string `json:"host"`
				DstIP       string `json:"destinationIP"`
				DstPort     string `json:"destinationPort"`
				SrcIP       string `json:"sourceIP"`
				SrcPort     string `json:"sourcePort"`
				Net         string `json:"network"`
				Process     string `json:"process"`
				ProcessPath string `json:"processPath"`
			} `json:"metadata"`
			Rule        string    `json:"rule"`
			RulePayload string    `json:"rulePayload"`
			Chains      []string  `json:"chains"`
			Upload      int64     `json:"upload"`
			Download    int64     `json:"download"`
			Start       time.Time `json:"start"`
		} `json:"connections"`
	}
	if err := r.request(http.MethodGet, "/connections", nil, &out); err != nil {
		return nil, err
	}
	conns := make([]core.Conn, 0, len(out.Connections))
	for _, c := range out.Connections {
		host := c.Metadata.Host
		if host == "" {
			host = c.Metadata.DstIP
		}
		conns = append(conns, core.Conn{
			ID: c.ID, Host: host, Network: c.Metadata.Net, Process: c.Metadata.Process,
			ProcessPath: c.Metadata.ProcessPath, SourceIP: c.Metadata.SrcIP,
			SourcePort: c.Metadata.SrcPort, DestinationIP: c.Metadata.DstIP,
			DestinationPort: c.Metadata.DstPort, Rule: c.Rule, RulePayload: c.RulePayload,
			Chains: c.Chains, Chain: strings.Join(c.Chains, " » "), Up: c.Upload, Down: c.Download, Start: c.Start,
		})
	}
	sort.SliceStable(conns, func(i, j int) bool { return conns[i].Start.After(conns[j].Start) })
	return conns, nil
}

func (r *Remote) CloseConnection(id string) error {
	if id == "" {
		return fmt.Errorf("%s", localize.Text("连接缺少标识，请刷新后重试"))
	}
	return r.request(http.MethodDelete, "/connections/"+url.PathEscape(id), nil, nil)
}

func (r *Remote) CloseAllConnections() error {
	return r.request(http.MethodDelete, "/connections", nil, nil)
}

func (r *Remote) Groups() []core.Group {
	var out struct {
		Proxies map[string]struct {
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			Now     string   `json:"now"`
			All     []string `json:"all"`
			Hidden  bool     `json:"hidden"`
			History []struct {
				Delay int `json:"delay"`
			} `json:"history"`
		} `json:"proxies"`
	}
	if err := r.request(http.MethodGet, "/proxies", nil, &out); err != nil {
		r.setErr(err)
		return nil
	}

	groups := make([]core.Group, 0, 4)
	for name, p := range out.Proxies {
		if p.All == nil {
			continue // a plain proxy, not a group
		}
		g := core.Group{Name: name, Type: p.Type, Now: p.Now, Members: p.All, Hidden: p.Hidden,
			Delays: map[string]uint16{}, MemberTypes: map[string]string{}}
		for _, member := range p.All {
			if node, ok := out.Proxies[member]; ok {
				g.MemberTypes[member] = node.Type
				if n := len(node.History); n > 0 {
					d := node.History[n-1].Delay
					if d >= 0 && d <= 65535 {
						g.Delays[member] = uint16(d)
					}
				}
			}
		}
		groups = append(groups, g)
	}
	// Mihomo's generated GLOBAL retains the subscription's group order.
	sortGroups(groups, out.Proxies["GLOBAL"].All)
	return groups
}

func (r *Remote) Rules() []core.Rule {
	var out struct {
		Rules []struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
			Proxy   string `json:"proxy"`
		} `json:"rules"`
	}
	if err := r.request(http.MethodGet, "/rules", nil, &out); err != nil {
		r.setErr(err)
		return nil
	}
	rules := make([]core.Rule, 0, len(out.Rules))
	for _, ru := range out.Rules {
		rules = append(rules, core.Rule{Type: ru.Type, Payload: ru.Payload, Adapter: ru.Proxy})
	}
	return rules
}

func (r *Remote) Mode() string {
	var out struct {
		Mode string `json:"mode"`
	}
	if err := r.request(http.MethodGet, "/configs", nil, &out); err != nil {
		r.setErr(err)
		return "rule"
	}
	return strings.ToLower(out.Mode)
}

func (r *Remote) Counts() (proxies, rules, listeners int) {
	var p struct {
		Proxies map[string]json.RawMessage `json:"proxies"`
	}
	if err := r.request(http.MethodGet, "/proxies", nil, &p); err == nil {
		proxies = len(p.Proxies)
	}
	return proxies, len(r.Rules()), 0
}

func (r *Remote) Delay(name, testURL string) (uint16, error) {
	return r.DelayContext(context.Background(), name, testURL, 5000)
}

// DelayContext uses the caller's measurement timeout and cancellation, without
// changing the shared timeout used for ordinary controller requests.
func (r *Remote) DelayContext(ctx context.Context, name, testURL string, timeoutMS int) (uint16, error) {
	if timeoutMS <= 0 {
		timeoutMS = 6000
	}
	if testURL == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS+1000)*time.Millisecond)
	defer cancel()
	q := url.Values{}
	q.Set("url", testURL)
	q.Set("timeout", fmt.Sprint(timeoutMS))
	path := "/proxies/" + url.PathEscape(name) + "/delay?" + q.Encode()

	var out struct {
		Delay   uint16 `json:"delay"`
		Message string `json:"message"`
	}
	client := *r.call
	client.Timeout = 0
	if err := r.requestContext(ctx, &client, http.MethodGet, path, nil, &out); err != nil {
		return 0, err
	}
	if out.Delay == 0 && out.Message != "" {
		return 0, fmt.Errorf("%s", out.Message)
	}
	return out.Delay, nil
}

// ---------- writes ----------

func (r *Remote) Select(group, member string) error {
	body, _ := json.Marshal(map[string]string{"name": member})
	return r.request(http.MethodPut, "/proxies/"+url.PathEscape(group), strings.NewReader(string(body)), nil)
}

func (r *Remote) SetMode(mode string) error {
	body, _ := json.Marshal(map[string]string{"mode": mode})
	return r.request(http.MethodPatch, "/configs", strings.NewReader(string(body)), nil)
}

// Reload sends the saved configuration through mihomo's payload API. The
// runtime file lives next to the kernel home, so passing its path would fail
// mihomo's safe-path check.
func (r *Remote) Reload(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf(localize.Text("读取运行配置: %w"), err)
	}
	body, _ := json.Marshal(map[string]string{"payload": string(raw)})
	// Large subscriptions re-parse and re-bind listeners; the shared 6s client
	// timeout made Start fail even after the daemon was already healthy.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := *r.call
	client.Timeout = 0
	return r.requestContext(ctx, &client, http.MethodPut, "/configs?force=true", strings.NewReader(string(body)), nil)
}

// ---------- logs ----------

// QueryDNS checks the active kernel resolver without changing its settings.
func (r *Remote) QueryDNS(ctx context.Context, name string) error {
	for _, kind := range []string{"A", "AAAA"} {
		var response struct {
			Status int
			Answer []struct {
				Type int `json:"type"`
			}
		}
		if err := r.requestContext(ctx, r.call, http.MethodGet, "/dns/query?"+url.Values{"name": {name}, "type": {kind}}.Encode(), nil, &response); err != nil {
			return err
		}
		if response.Status != 0 {
			return fmt.Errorf(localize.Text("DNS 返回状态 %d"), response.Status)
		}
		for _, answer := range response.Answer {
			if answer.Type == 1 || answer.Type == 28 {
				return nil
			}
		}
	}
	return fmt.Errorf("%s", localize.Text("DNS 未返回地址"))
}

func (r *Remote) Logs() []core.LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.LogLine, len(r.logs))
	copy(out, r.logs)
	return out
}

func (r *Remote) ClearLogs() {
	r.mu.Lock()
	r.logs = nil
	r.mu.Unlock()
}

func (r *Remote) setErr(err error) {
	r.mu.Lock()
	r.lastErr = err.Error()
	r.mu.Unlock()
}

func (r *Remote) LastError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
