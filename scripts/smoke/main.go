// Command smoke checks an isolated, real core without opening a UI, installing
// a service, reading user profiles, or changing the system proxy.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/kernel"
	"gopurr/internal/process"
)

func main() {
	binary := flag.String("binary", "bin/Gopurr", "built application executable")
	flag.Parse()
	if err := run(*binary); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
	fmt.Println("PASS: isolated core verified download, upload, and traffic counters")
}

func freeMixedPort() (int, error) {
	for i := 0; i < 20; i++ {
		tcp, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		port := tcp.Addr().(*net.TCPAddr).Port
		udp, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		tcp.Close()
		if err == nil {
			udp.Close()
			return port, nil
		}
	}
	return 0, errors.New("no available TCP/UDP port")
}

func run(binary string) (result error) {
	exe, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "gp-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	port, err := freeMixedPort()
	if err != nil {
		return err
	}
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(secretBytes); err != nil {
		return err
	}
	secret := hex.EncodeToString(secretBytes)
	secretPath := filepath.Join(dir, "core.secret")
	if err = os.WriteFile(secretPath, []byte(secret), 0600); err != nil {
		return err
	}
	config := fmt.Sprintf("mixed-port: %d\nallow-lan: false\nmode: rule\nlog-level: warning\ngeo-auto-update: false\ndns:\n  enable: false\ntun:\n  enable: false\nhosts:\n  gopurr-smoke.invalid: 127.0.0.1\nrules:\n  - MATCH,DIRECT\n", port)
	configPath, socket, statusPath := filepath.Join(dir, "runtime.yaml"), filepath.Join(dir, "core.sock"), filepath.Join(dir, "core.json")
	if err = os.WriteFile(configPath, []byte(config), 0600); err != nil {
		return err
	}
	logPath := filepath.Join(dir, "core.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logFile.Close()
	defer func() {
		if result != nil {
			raw, _ := os.ReadFile(logPath)
			if len(raw) > 4096 {
				raw = raw[len(raw)-4096:]
			}
			fmt.Fprintln(os.Stderr, string(raw))
		}
	}()
	cmd := exec.Command(exe, "--core", "--config", configPath, "--socket", socket, "--secret-file", secretPath, "--home", filepath.Join(dir, "mihomo"), "--status", statusPath, "--parent", strconv.Itoa(os.Getpid()))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	process.Configure(cmd)
	if err = cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	defer func() {
		if exited {
			return
		}
		_ = process.Stop(cmd.Process)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	remote := kernel.NewRemote(socket, secret)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		var status core.DaemonStatus
		data, _ := os.ReadFile(statusPath)
		if json.Unmarshal(data, &status) == nil && status.PID == cmd.Process.Pid && remote.Probe(ctx) == nil {
			break
		}
		select {
		case err := <-done:
			exited = true
			return fmt.Errorf("test core exited before readiness: %v", err)
		case <-ctx.Done():
			return fmt.Errorf("test core readiness: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	runtime, err := remote.Runtime(ctx)
	if err != nil || !runtime.ProxyReady || runtime.MixedPort != port || runtime.TunEnabled {
		return fmt.Errorf("unexpected isolated runtime: %+v: %v", runtime, err)
	}
	download := bytes.Repeat([]byte{0xa5}, 4<<20)
	upload := bytes.Repeat([]byte{0x5a}, 64<<10)
	uploaded := make(chan []byte, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/blob":
			_, _ = w.Write(download)
		case r.Method == "POST" && r.URL.Path == "/upload":
			data, err := io.ReadAll(io.LimitReader(r.Body, int64(len(upload)+1)))
			if err != nil || !bytes.Equal(data, upload) {
				http.Error(w, "bad upload", 400)
				return
			}
			uploaded <- data
			sum := sha256.Sum256(data)
			fmt.Fprintf(w, "%x", sum)
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	target := strings.Replace(origin.URL, "127.0.0.1", "gopurr-smoke.invalid", 1)
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	before, err := traffic(socket, secret)
	if err != nil {
		return err
	}
	resp, err := client.Get(target + "/blob")
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil || resp.StatusCode != 200 || !bytes.Equal(data, download) {
		return errors.New("download body/status mismatch")
	}
	resp, err = client.Post(target+"/upload", "application/octet-stream", bytes.NewReader(upload))
	if err != nil {
		return err
	}
	data, readErr = io.ReadAll(resp.Body)
	resp.Body.Close()
	expected := sha256.Sum256(upload)
	if readErr != nil || resp.StatusCode != 200 || string(data) != fmt.Sprintf("%x", expected) {
		return errors.New("upload receipt/status mismatch")
	}
	select {
	case got := <-uploaded:
		if !bytes.Equal(got, upload) {
			return errors.New("origin did not receive exact upload")
		}
	default:
		return errors.New("upload bypassed test origin")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		after, err := traffic(socket, secret)
		if err != nil {
			return err
		}
		if after.DownTotal-before.DownTotal >= int64(len(download)) && after.UpTotal-before.UpTotal >= int64(len(upload)) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("traffic was not accounted by test core: before=%+v after=%+v", before, after)
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("Verified core PID %d, private port %d, download %d B, upload %d B\n", cmd.Process.Pid, port, len(download), len(upload))
	return nil
}

func traffic(socket, secret string) (core.Sample, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	req, _ := http.NewRequest("GET", "http://localhost/traffic", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := client.Do(req)
	if err != nil {
		return core.Sample{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return core.Sample{}, fmt.Errorf("traffic HTTP %d", resp.StatusCode)
	}
	var sample struct {
		UpTotal   int64 `json:"upTotal"`
		DownTotal int64 `json:"downTotal"`
	}
	err = json.NewDecoder(resp.Body).Decode(&sample)
	return core.Sample{UpTotal: sample.UpTotal, DownTotal: sample.DownTotal}, err
}
