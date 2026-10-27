//go:build darwin

package sysproxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type networkFixture struct {
	mu      sync.Mutex
	service string
	proxies [3]proxySetting
	bypass  []string
	fail    string
}

func newNetworkFixture() *networkFixture {
	return &networkFixture{
		service: "Wi-Fi",
		proxies: [3]proxySetting{{true, "http.example", 8080}, {true, "https.example", 8443}, {false, "socks.example", 1080}},
		bypass:  []string{"*.corp", "localhost"},
	}
}

func (n *networkFixture) run(name string, args ...string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.fail != "" && strings.Join(args, " ") == n.fail {
		n.fail = ""
		return "", errors.New("fixture command failed")
	}
	if name == "route" {
		if n.service == "Ethernet" {
			return "interface: en1\n", nil
		}
		return "interface: en0\n", nil
	}
	if name != "networksetup" || len(args) == 0 {
		return "", errors.New("unexpected command")
	}
	if args[0] == "-listnetworkserviceorder" {
		return "(1) Wi-Fi\n(Hardware Port: Wi-Fi, Device: en0)\n(2) Ethernet\n(Hardware Port: Ethernet, Device: en1)\n", nil
	}
	// This fixture intentionally has no editable Ethernet configuration: quit
	// must restore the service captured at enable, not the new default route.
	if len(args) < 2 || args[1] != "Wi-Fi" {
		return "", errors.New("wrong network service")
	}
	for i, protocol := range proxyProtocols {
		switch args[0] {
		case "-get" + protocol + "proxy":
			enabled := "No"
			if n.proxies[i].enabled {
				enabled = "Yes"
			}
			return fmt.Sprintf("Enabled: %s\nServer: %s\nPort: %d\n", enabled, n.proxies[i].server, n.proxies[i].port), nil
		case "-set" + protocol + "proxy":
			port, _ := strconv.Atoi(args[3])
			n.proxies[i] = proxySetting{true, args[2], port}
			return "", nil
		case "-set" + protocol + "proxystate":
			n.proxies[i].enabled = args[2] == "on"
			return "", nil
		}
	}
	switch args[0] {
	case "-getproxybypassdomains":
		if len(n.bypass) == 0 {
			return "There aren't any bypass domains set on Wi-Fi.\n", nil
		}
		return strings.Join(n.bypass, "\n") + "\n", nil
	case "-setproxybypassdomains":
		n.bypass = append([]string(nil), args[2:]...)
		if len(n.bypass) == 1 && n.bypass[0] == "Empty" {
			n.bypass = nil
		}
		return "", nil
	}
	return "", fmt.Errorf("unexpected operation %s", args[0])
}

func (n *networkFixture) assertOriginal(t *testing.T) {
	t.Helper()
	n.mu.Lock()
	defer n.mu.Unlock()
	want := newNetworkFixture()
	if n.proxies != want.proxies || !reflect.DeepEqual(n.bypass, want.bypass) {
		t.Fatalf("proxy configuration not restored: proxies=%+v bypass=%v", n.proxies, n.bypass)
	}
}

func testHelperController(t *testing.T, network *networkFixture) (*helperController, *int, <-chan error) {
	t.Helper()
	opens := 0
	done := make(chan error, 1)
	c := &helperController{open: func() (net.Conn, error) {
		opens++
		client, helper := net.Pipe()
		go func() {
			defer helper.Close()
			done <- serveProxyHelper(helper, network.run)
		}()
		return client, nil
	}}
	t.Cleanup(func() {
		if c.conn != nil {
			c.conn.Close()
		}
	})
	return c, &opens, done
}

func waitHelper(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy helper did not stop")
	}
}

func TestProxySessionAuthorizesOnceAcrossSwitchesAndQuit(t *testing.T) {
	network := newNetworkFixture()
	c, opens, done := testHelperController(t, network)
	for _, port := range []int{7890, 7900} {
		if err := c.Enable(port, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Restore(nil); err != nil {
		t.Fatal(err)
	}
	network.assertOriginal(t)
	if err := c.Enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	waitHelper(t, done)
	network.assertOriginal(t)
	if *opens != 1 {
		t.Fatalf("authorized %d sessions, want one", *opens)
	}
}

func TestProxyHelperRestoresAfterApplicationDisconnects(t *testing.T) {
	network := newNetworkFixture()
	c, _, done := testHelperController(t, network)
	if err := c.Enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	c.conn.Close()
	waitHelper(t, done)
	network.assertOriginal(t)
}

func TestProxyRestoresOriginalServiceAfterNetworkSwitch(t *testing.T) {
	network := newNetworkFixture()
	c, _, done := testHelperController(t, network)
	if err := c.Enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	network.mu.Lock()
	network.service = "Ethernet"
	network.mu.Unlock()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	waitHelper(t, done)
	network.assertOriginal(t)
}

func TestProxyRestorePreservesAnotherClientsChanges(t *testing.T) {
	network := newNetworkFixture()
	c, _, done := testHelperController(t, network)
	if err := c.Enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	other := proxySetting{true, "another.example", 3128}
	network.mu.Lock()
	network.proxies[0] = other
	network.bypass = []string{"*.other"}
	network.mu.Unlock()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	waitHelper(t, done)
	if network.proxies[0] != other || !reflect.DeepEqual(network.bypass, []string{"*.other"}) || network.proxies[1] != newNetworkFixture().proxies[1] {
		t.Fatalf("other client's settings overwritten: %+v %v", network.proxies, network.bypass)
	}
}

func TestProxyRestoreFailureCanRetryWithoutAuthorization(t *testing.T) {
	network := newNetworkFixture()
	c, opens, done := testHelperController(t, network)
	if err := c.Enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	network.mu.Lock()
	network.fail = "-setsocksfirewallproxystate Wi-Fi off"
	network.mu.Unlock()
	if err := c.Restore(nil); err == nil {
		t.Fatal("restore failure hidden")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	waitHelper(t, done)
	network.assertOriginal(t)
	if *opens != 1 {
		t.Fatal("restore retry required another authorization")
	}
}

func TestProxyPartialEnableFailureRollsBack(t *testing.T) {
	network := newNetworkFixture()
	network.fail = "-setsecurewebproxy Wi-Fi 127.0.0.1 7890"
	c, _, done := testHelperController(t, network)
	if err := c.Enable(7890, nil); err == nil {
		t.Fatal("partial enable failure hidden")
	}
	network.assertOriginal(t)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	waitHelper(t, done)
}

func TestProxyRecoversLegacyOwnership(t *testing.T) {
	network := newNetworkFixture()
	for i := range network.proxies {
		network.proxies[i] = proxySetting{true, "127.0.0.1", 7890}
	}
	c, _, done := testHelperController(t, network)
	previous := &State{Enabled: true, Service: "Wi-Fi", Server: "legacy.example", Port: 8080}
	if err := c.Enable(7890, previous); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	waitHelper(t, done)
	for _, setting := range network.proxies {
		if setting != (proxySetting{true, previous.Server, previous.Port}) {
			t.Fatalf("legacy backup not restored: %+v", setting)
		}
	}
}

func TestProxyHelperRejectsInvalidPortBeforeAuthorization(t *testing.T) {
	c := &helperController{open: func() (net.Conn, error) { t.Fatal("unexpected authorization"); return nil, nil }}
	for _, port := range []int{-1, 0, 65536} {
		if err := c.Enable(port, nil); err == nil {
			t.Fatalf("accepted port %d", port)
		}
	}
}

func TestProxyAuthorizationCancellationDoesNotStartASession(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte("#!/bin/sh\nprintf 'User canceled. (-128)\\n'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	c := NewController().(*helperController)
	if err := c.Enable(7890, nil); err == nil || err.Error() != "已取消授权" || c.conn != nil {
		t.Fatalf("canceled authorization did not stop cleanly: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProxyRestoreNeverAuthorizesAMissingSession(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"route":        "#!/bin/sh\nprintf 'interface: en0\\n'\n",
		"networksetup": "#!/bin/sh\ncase \"$1\" in\n-listnetworkserviceorder) printf '(1) Wi-Fi\\n(Hardware Port: Wi-Fi, Device: en0)\\n';;\n-getwebproxy) printf 'Enabled: Yes\\nServer: 127.0.0.1\\nPort: 7890\\n';;\nesac\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	c := &helperController{port: 7890, open: func() (net.Conn, error) {
		t.Fatal("quit tried to authorize a new helper")
		return nil, nil
	}}
	if err := c.Restore(nil); err == nil {
		t.Fatal("lost session should keep the app running for recovery")
	}
}

func TestProxyHelperRejectsUnknownOperations(t *testing.T) {
	network := newNetworkFixture()
	c, _, done := testHelperController(t, network)
	conn, err := c.open()
	if err != nil {
		t.Fatal(err)
	}
	c.conn, c.input = conn, helperScanner(conn)
	if err := c.request(helperRequest{Operation: "shell"}); err == nil {
		t.Fatal("helper accepted an arbitrary operation")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	waitHelper(t, done)
	network.assertOriginal(t)
}

func TestProxyHelperValidatesSocketPeer(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gp-peer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "helper.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	conn, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := checkHelperPeer(conn, os.Geteuid(), os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := checkHelperPeer(conn, os.Geteuid()+1, 0); err == nil {
		t.Fatal("accepted wrong uid")
	}
	if err := checkHelperPeer(conn, -1, os.Getpid()+1); err == nil {
		t.Fatal("accepted wrong pid")
	}
}
