package ws

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func clientFrame(op byte, p []byte) []byte {
	mask := make([]byte, 4)
	rand.Read(mask)
	f := []byte{0x80 | op, 0x80 | byte(len(p))}
	f = append(f, mask...)
	for i, b := range p {
		f = append(f, b^mask[i&3])
	}
	return f
}

func TestEcho(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Upgrade(w, r)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			m, err := c.ReadMessage()
			if err != nil {
				return
			}
			c.WriteText(append([]byte("echo:"), m...))
		}
	}))
	defer srv.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	key := make([]byte, 16)
	rand.Read(key)
	conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(key) + "\r\n\r\n"))
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != 101 {
		t.Fatalf("handshake: %v %v", err, resp)
	}
	conn.Write(clientFrame(opPing, []byte("p")))
	conn.Write(clientFrame(opText, []byte("hello")))
	got := map[byte]string{}
	for i := 0; i < 2; i++ {
		h := make([]byte, 2)
		if _, err := br.Read(h[:1]); err != nil {
			t.Fatal(err)
		}
		br.Read(h[1:])
		p := make([]byte, h[1]&0x7f)
		br.Read(p)
		got[h[0]&0x0f] = string(p)
	}
	if got[opPong] != "p" || got[opText] != "echo:hello" {
		t.Fatalf("got %v", got)
	}
}

func TestRejectUnmasked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Upgrade(w, r)
		if err != nil {
			return
		}
		if _, err := c.ReadMessage(); err == nil {
			t.Error("unmasked frame accepted")
		}
		c.Close()
	}))
	defer srv.Close()
	conn, _ := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	defer conn.Close()
	conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"))
	br := bufio.NewReader(conn)
	http.ReadResponse(br, nil)
	conn.Write([]byte{0x81, 0x02, 'h', 'i'})
	br.ReadByte()
}
