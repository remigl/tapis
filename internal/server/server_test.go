package server

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/remigl/tapis/internal/certs"
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

func TestValidHost(t *testing.T) {
	s := &Server{Identity: &certs.Identity{LeafCert: &x509.Certificate{Subject: pkix.Name{CommonName: "desktop.local"}}}}
	_, lan, _ := net.ParseCIDR("192.168.1.0/24")
	s.nets, s.ownIPs, s.netsLoaded = []*net.IPNet{lan}, []net.IP{net.ParseIP("192.168.1.10")}, time.Now()
	for h, want := range map[string]bool{
		"192.168.1.10:47800": true, "127.0.0.1:47800": true, "[::1]:47800": true, "localhost:47800": true,
		"desktop.local:47800": true, "evil.example:47800": false, "8.8.8.8:47800": false,
		"192.168.1.10.nip.io:47800": false, "": false,
		"192.168.1.11:47800": false, // another machine's address: not us
		"10.0.0.5:47800":     false,
	} {
		if got := s.validHost(h); got != want {
			t.Errorf("validHost(%q) = %v, want %v", h, got, want)
		}
	}
}

func TestAllowedClient(t *testing.T) {
	s := &Server{}
	_, lan, _ := net.ParseCIDR("192.168.1.0/24")
	s.nets, s.ownIPs, s.netsLoaded = []*net.IPNet{lan}, []net.IP{net.ParseIP("192.168.1.10")}, time.Now()
	for ip, want := range map[string]bool{
		"192.168.1.57": true, "127.0.0.1": true, "::1": true,
		"192.168.2.57": false, // another subnet (router, guest network)
		"10.8.0.2":     false, // a VPN peer: private, but not our Wi-Fi
		"172.17.0.2":   false, // a container network
		"8.8.8.8":      false,
	} {
		if got := s.allowedClient(net.ParseIP(ip)); got != want {
			t.Errorf("allowedClient(%s) = %v, want %v", ip, got, want)
		}
	}
	if s.allowedClient(nil) {
		t.Error("nil address allowed")
	}
}
