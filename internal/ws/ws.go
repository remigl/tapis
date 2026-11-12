// Package ws is a minimal RFC 6455 WebSocket server implementation, using
// only the standard library. It supports text messages, ping/pong and close,
// which is all Tapis needs. Client frames must be masked (as browsers do).
package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// MaxMessage is the largest message (after reassembly) we accept.
const MaxMessage = 64 << 10

const (
	opCont  = 0x0
	opText  = 0x1
	opBin   = 0x2
	opClose = 0x8
	opPing  = 0x9
	opPong  = 0xA
)

var ErrClosed = errors.New("ws: connection closed")

// Conn is a server-side WebSocket connection.
type Conn struct {
	c   net.Conn
	br  *bufio.Reader
	wmu sync.Mutex
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// Upgrade performs the WebSocket handshake. The caller is responsible for
// checking the Origin header before calling it.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if r.Method != http.MethodGet || !headerHas(r.Header, "Connection", "upgrade") ||
		!headerHas(r.Header, "Upgrade", "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return nil, errors.New("ws: bad handshake")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if k, err := base64.StdEncoding.DecodeString(key); err != nil || len(k) != 16 {
		http.Error(w, "bad Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, errors.New("ws: bad key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return nil, errors.New("ws: hijack unsupported")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(key + guid))
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n"
	conn.SetDeadline(time.Time{})
	if _, err := conn.Write([]byte(resp)); err != nil {
		conn.Close()
		return nil, err
	}
	return &Conn{c: conn, br: brw.Reader}, nil
}

// SetReadDeadline sets the deadline for the next ReadMessage.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.c.SetReadDeadline(t) }

// RemoteAddr returns the peer address.
func (c *Conn) RemoteAddr() net.Addr { return c.c.RemoteAddr() }

func (c *Conn) readFrame() (fin bool, op byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(c.br, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	if h[0]&0x70 != 0 {
		return false, 0, nil, errors.New("ws: reserved bits set")
	}
	op = h[0] & 0x0F
	if h[1]&0x80 == 0 {
		return false, 0, nil, errors.New("ws: unmasked client frame")
	}
	n := uint64(h[1] & 0x7F)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	if n > MaxMessage {
		return false, 0, nil, errors.New("ws: frame too large")
	}
	var mask [4]byte
	if _, err = io.ReadFull(c.br, mask[:]); err != nil {
		return
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	for i := range payload {
		payload[i] ^= mask[i&3]
	}
	return
}

// ReadMessage returns the next text or binary message, transparently
// answering pings and handling close frames.
func (c *Conn) ReadMessage() ([]byte, error) {
	var msg []byte
	started := false
	for {
		fin, op, p, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch op {
		case opPing:
			if err := c.writeFrame(opPong, p); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			c.writeFrame(opClose, nil)
			return nil, ErrClosed
		case opText, opBin:
			if started {
				return nil, errors.New("ws: unexpected new message")
			}
			started = true
			msg = p
		case opCont:
			if !started {
				return nil, errors.New("ws: unexpected continuation")
			}
			if len(msg)+len(p) > MaxMessage {
				return nil, errors.New("ws: message too large")
			}
			msg = append(msg, p...)
		default:
			return nil, errors.New("ws: unknown opcode")
		}
		if fin {
			return msg, nil
		}
	}
}

func (c *Conn) writeFrame(op byte, p []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	hdr := make([]byte, 0, 10)
	hdr = append(hdr, 0x80|op)
	switch n := len(p); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	c.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.c.Write(append(hdr, p...)); err != nil {
		return err
	}
	return nil
}

// WriteText sends a text message.
func (c *Conn) WriteText(p []byte) error { return c.writeFrame(opText, p) }

// Ping sends a ping frame.
func (c *Conn) Ping() error { return c.writeFrame(opPing, nil) }

// Close closes the connection with a normal-closure frame.
func (c *Conn) Close() error {
	c.writeFrame(opClose, []byte{0x03, 0xE8})
	return c.c.Close()
}
