package app

import (
	"net"
	"testing"
)

func TestMixedPortRejectsOccupiedUDP(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.LocalAddr().(*net.UDPAddr).Port
	if portFree(port) {
		t.Fatal("UDP-only port conflict was accepted by mixed-port preflight")
	}
}
