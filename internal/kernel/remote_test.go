package kernel

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeController speaks the mihomo controller protocol on a unix socket, so the
// client can be tested without a real kernel.
type fakeController struct {
	srv  *http.Server
	ln   net.Listener
	path string
	t    *testing.T
	mu   sync.Mutex
	body string
}

func (f *fakeController) handler(secret string) http.Handler {
	mux := http.NewServeMux()

	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}

	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"version":"1.10.0","meta":true}`))
	})

	mux.HandleFunc("/proxies", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"proxies":{
			"节点选择":{"name":"节点选择","type":"Selector","now":"DIRECT","all":["DIRECT","香港 01"]},
			"香港 01":{"name":"香港 01","type":"Shadowsocks"},
			"DIRECT":{"name":"DIRECT","type":"Direct"}
		}}`))
	})

	mux.HandleFunc("/proxies/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/delay") {
			if r.URL.Query().Get("url") == "" {
				f.t.Error("delay without url")
			}
			_, _ = w.Write([]byte(`{"delay":42}`))
			return
		}
		if r.Method == http.MethodPut {
			var in struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.mu.Lock()
			f.body = in.Name
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	mux.HandleFunc("/rules", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"rules":[
			{"index":0,"type":"DomainSuffix","payload":"local","proxy":"DIRECT"},
			{"index":1,"type":"Match","payload":"","proxy":"节点选择"}
		]}`))
	})

	mux.HandleFunc("/configs", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.Method == http.MethodPatch {
			var in struct {
				Mode string `json:"mode"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.mu.Lock()
			f.body = in.Mode
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPut {
			var in struct {
				Path    string `json:"path"`
				Payload string `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Payload == "" || in.Path != "" {
				f.t.Error("reload must send configuration content, not a path outside the kernel home")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.body = in.Payload
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"mixed-port":7890,"mode":"rule"}`))
	})

	mux.HandleFunc("/connections", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"downloadTotal":100,"uploadTotal":50,"connections":[
			{"metadata":{"host":"example.com","network":"tcp"},
			 "rule":"Match","chains":["节点选择","DIRECT"],
			 "upload":10,"download":20,"start":"2026-10-27T12:00:00.5+08:00"}
		]}`))
	})

	mux.HandleFunc("/traffic", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			f.t.Error("no flusher")
			return
		}
		for _, line := range []string{
			`{"up":11,"down":22,"upTotal":100,"downTotal":200}`,
			`{"up":33,"down":44,"upTotal":300,"downTotal":400}`,
		} {
			_, _ = w.Write([]byte(line + "\n"))
			flusher.Flush()
		}
		<-r.Context().Done()
	})

	mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`{"type":"info","payload":"listening at 127.0.0.1:7890"}` + "\n"))
		flusher.Flush()
		<-r.Context().Done()
	})

	return mux
}

func startFake(t *testing.T, secret string) *fakeController {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeController{ln: ln, t: t}
	f.srv = &http.Server{Handler: f.handler(secret)}
	go func() { _ = f.srv.Serve(ln) }()
	t.Cleanup(func() { _ = f.srv.Close() })
	f.path = path
	return f
}

// path is kept separately so the test can build a Remote for it.
func (f *fakeController) socket() string { return f.path }

func TestRemoteReadsAndWrites(t *testing.T) {
	f := startFake(t, "s3cret")
	r := NewRemote(f.socket(), "s3cret")

	if err := r.Probe(t.Context()); err != nil {
		t.Fatalf("probe: %v", err)
	}

	groups := r.Groups()
	if len(groups) != 1 || groups[0].Name != "节点选择" {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].Now != "DIRECT" || len(groups[0].Members) != 2 {
		t.Fatalf("group detail = %+v", groups[0])
	}

	if err := r.Select("节点选择", "香港 01"); err != nil {
		t.Fatalf("select: %v", err)
	}
	f.mu.Lock()
	if f.body != "香港 01" {
		t.Fatalf("select body = %q", f.body)
	}
	f.mu.Unlock()

	d, err := r.Delay("香港 01", "http://example.com")
	if err != nil || d != 42 {
		t.Fatalf("delay = %d, %v", d, err)
	}

	rules := r.Rules()
	if len(rules) != 2 || rules[1].Adapter != "节点选择" || rules[0].Type != "DomainSuffix" {
		t.Fatalf("rules = %+v", rules)
	}

	if got := r.Mode(); got != "rule" {
		t.Fatalf("mode = %q", got)
	}
	if err := r.SetMode("global"); err != nil {
		t.Fatalf("set mode: %v", err)
	}
	f.mu.Lock()
	if f.body != "global" {
		t.Fatalf("mode body = %q", f.body)
	}
	f.mu.Unlock()

	conns := r.Connections(10)
	if len(conns) != 1 || conns[0].Host != "example.com" {
		t.Fatalf("connections = %+v", conns)
	}
	if conns[0].Chain != "节点选择 » DIRECT" {
		t.Fatalf("chain = %q", conns[0].Chain)
	}
	if conns[0].Up != 10 || conns[0].Down != 20 {
		t.Fatalf("counters = %+v", conns[0])
	}
}

func TestRemoteRejectsBadSecret(t *testing.T) {
	f := startFake(t, "right")
	r := NewRemote(f.socket(), "wrong")
	if err := r.Probe(t.Context()); err == nil {
		t.Fatal("expected auth failure")
	}
}

func TestRemoteStreams(t *testing.T) {
	f := startFake(t, "s")
	r := NewRemote(f.socket(), "s")
	r.Watch()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := r.Sample(); s.DownNow == 44 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s := r.Sample(); s.DownNow != 44 || s.DownTotal != 400 {
		t.Fatalf("traffic = %+v", s)
	}

	for time.Now().Before(deadline) {
		if len(r.Logs()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	logs := r.Logs()
	if len(logs) == 0 || !strings.Contains(logs[0].Text, "listening") {
		t.Fatalf("logs = %+v", logs)
	}

	r.ClearLogs()
	if len(r.Logs()) != 0 {
		t.Fatal("logs not cleared")
	}
}

func TestRemoteReloadPayload(t *testing.T) {
	f := startFake(t, "s")
	r := NewRemote(f.socket(), "s")
	path := filepath.Join(t.TempDir(), "runtime.yaml")
	const content = "mode: rule\nproxy-groups: []\nrules: [MATCH,DIRECT]\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.Reload(path); err != nil {
		t.Fatalf("reload: %v", err)
	}
	f.mu.Lock()
	got := f.body
	f.mu.Unlock()
	if got != content {
		t.Fatalf("reload payload = %q, want %q", got, content)
	}
	if err := r.Reload(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected an error for an unreadable config")
	}
}
