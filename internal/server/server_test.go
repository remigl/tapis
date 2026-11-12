package server

import (
	"net"
	"testing"
)

func TestIsLocal(t *testing.T) {
	for ip, want := range map[string]bool{
		"192.168.1.20": true, "10.0.0.5": true, "172.20.1.1": true, "127.0.0.1": true,
		"169.254.3.4": true, "fd00::1": true, "8.8.8.8": false, "100.64.1.1": false,
		"2001:4860:4860::8888": false,
	} {
		if got := IsLocal(net.ParseIP(ip)); got != want {
			t.Errorf("IsLocal(%s) = %v, want %v", ip, got, want)
		}
	}
}
