package server

import (
	"log"
	"net"
	"strings"
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

func TestHandshakeLog(t *testing.T) {
	var buf strings.Builder
	s := &Server{cfg: Config{Log: log.New(&buf, "", 0)}}
	h := &handshakeLog{s: s}
	h.Write([]byte("http: TLS handshake error from 192.168.1.23:51234: remote error: tls: unknown certificate\n"))
	h.Write([]byte("http: TLS handshake error from 192.168.1.23:51235: remote error: tls: unknown certificate\n"))
	if got := strings.Count(buf.String(), "192.168.1.23 reached Tapis"); got != 1 {
		t.Fatalf("got %d notes: %q", got, buf.String())
	}
}
