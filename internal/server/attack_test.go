package server

// Regression tests that attack a real running server (the same checks as
// the security audit). They use loopback addresses 127.0.0.x as distinct
// "attackers".

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/remigl/tapis/internal/input"
)

type logBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuf) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.b.Write(p) }
func (l *logBuf) String() string              { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func startServer(t *testing.T) (string, *logBuf, *logBuf) {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	logs, events := &logBuf{}, &logBuf{}
	s, err := New(Config{Port: port, Dir: t.TempDir(), Injector: &input.Logger{W: events},
		Web: fstest.MapFS{"index.html": {Data: []byte("ok")}}, Log: log.New(logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Run(ctx)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.NewPairing()
	return addr, logs, events
}

func dialFrom(addr, src string) (*tls.Conn, error) {
	d := &net.Dialer{Timeout: 3 * time.Second}
	if src != "" {
		d.LocalAddr = &net.TCPAddr{IP: net.ParseIP(src)}
	}
	return tls.DialWithDialer(d, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
}

func wsOpen(addr, src, origin string) (net.Conn, *bufio.Reader, int) {
	c, err := dialFrom(addr, src)
	if err != nil {
		return nil, nil, -1
	}
	k := make([]byte, 16)
	rand.Read(k)
	fmt.Fprintf(c, "GET /ws HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n",
		addr, origin, base64.StdEncoding.EncodeToString(k))
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		c.Close()
		return nil, nil, -2
	}
	return c, br, resp.StatusCode
}

func maskedFrame(op byte, p []byte) []byte {
	m := []byte{1, 2, 3, 4}
	f := []byte{0x80 | op, 0x80 | byte(len(p))}
	f = append(f, m...)
	for i, b := range p {
		f = append(f, b^m[i&3])
	}
	return f
}

func readFrame(br *bufio.Reader) (byte, string) {
	h := make([]byte, 2)
	if _, err := io.ReadFull(br, h); err != nil {
		return 0, ""
	}
	p := make([]byte, int(h[1]&0x7f))
	io.ReadFull(br, p)
	return h[0], string(p)
}

func TestAttacks(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses 127.0.0.x source addresses (Linux)")
	}
	addr, logs, events := startServer(t)
	origin := "https://" + addr
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}

	t.Run("foreign Host refused (DNS rebinding)", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "https://"+addr+"/", nil)
		req.Host = "attacker.example"
		r, err := hc.Do(req)
		if err != nil || r.StatusCode != 421 {
			t.Fatalf("got %v %v", r, err)
		}
	})
	t.Run("cross-origin WebSocket refused", func(t *testing.T) {
		if _, _, st := wsOpen(addr, "", "https://evil.example"); st != 403 {
			t.Fatalf("status %d", st)
		}
	})
	t.Run("huge headers refused", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "https://"+addr+"/", nil)
		req.Header.Set("X-Big", strings.Repeat("a", 64<<10))
		if r, err := hc.Do(req); err == nil && r.StatusCode != 431 {
			t.Fatalf("status %d", r.StatusCode)
		}
	})
	t.Run("no command before authentication", func(t *testing.T) {
		c, br, _ := wsOpen(addr, "", origin)
		defer c.Close()
		c.Write(maskedFrame(1, []byte(`{"t":"move","x":123,"y":456}`)))
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if op, _ := readFrame(br); op != 0x88 {
			t.Fatalf("expected close frame, got %#x", op)
		}
		if strings.Contains(events.String(), "123") {
			t.Fatal("input was injected without authentication")
		}
	})
	t.Run("pairing code cannot be guessed from many addresses", func(t *testing.T) {
		for i := 0; i < 25; i++ {
			c, br, st := wsOpen(addr, fmt.Sprintf("127.0.1.%d", 2+i), origin)
			if st != 101 {
				continue
			}
			c.Write(maskedFrame(1, []byte(`{"t":"pair","token":"AAAAAAAAAAAAAAAAAAAAAA"}`)))
			c.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, msg := readFrame(br); strings.Contains(msg, "paired") {
				t.Fatal("wrong code accepted")
			}
			c.Close()
		}
		if !strings.Contains(logs.String(), "too many wrong pairing codes") {
			t.Fatal("code not cancelled after repeated guesses")
		}
	})
	t.Run("key guessing locked out", func(t *testing.T) {
		for i := 0; i < 8; i++ {
			c, br, st := wsOpen(addr, "127.0.2.2", origin)
			if st == 429 {
				return
			}
			if c != nil {
				c.Write(maskedFrame(1, []byte(`{"t":"auth","id":"x","key":"y"}`)))
				c.SetReadDeadline(time.Now().Add(2 * time.Second))
				readFrame(br)
				c.Close()
			}
		}
		t.Fatal("no lockout")
	})
	t.Run("WebSocket slots per address", func(t *testing.T) {
		codes := map[int]int{}
		var held []net.Conn
		for i := 0; i < 6; i++ {
			c, _, st := wsOpen(addr, "127.0.3.2", origin)
			codes[st]++
			if c != nil {
				held = append(held, c)
			}
		}
		for _, c := range held {
			c.Close()
		}
		if codes[101] != 4 {
			t.Fatalf("codes %v", codes)
		}
	})
	t.Run("connection flood from one address", func(t *testing.T) {
		var idle []net.Conn
		d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.4.2")}, Timeout: time.Second}
		for i := 0; i < 80; i++ {
			if c, err := d.Dial("tcp", addr); err == nil {
				idle = append(idle, c)
			}
		}
		time.Sleep(200 * time.Millisecond)
		open := 0
		for _, c := range idle {
			c.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
			if _, err := c.Read(make([]byte, 1)); err != nil && strings.Contains(err.Error(), "timeout") {
				open++
			}
			defer c.Close()
		}
		if open > maxRawPerIP {
			t.Fatalf("%d idle connections kept", open)
		}
		if c, err := dialFrom(addr, "127.0.4.3"); err != nil {
			t.Fatalf("server unavailable to others during flood: %v", err)
		} else {
			c.Close()
		}
	})
	t.Run("TLS 1.2 refused", func(t *testing.T) {
		if _, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS12}); err == nil {
			t.Fatal("TLS 1.2 accepted")
		}
	})
}
