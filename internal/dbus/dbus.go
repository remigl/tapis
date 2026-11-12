// Package dbus is a small D-Bus client written with the standard library
// only. It supports what Tapis needs for its Linux tray icon: connecting to
// the session bus, calling methods, exporting objects (answering method
// calls) and emitting signals.
//
// Values are written with an Encoder (explicit, typed calls) and read into
// generic Go values (int32, uint32, string, bool, []any, Variant…).
package dbus

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Message types.
const (
	TypeMethodCall   = 1
	TypeMethodReturn = 2
	TypeError        = 3
	TypeSignal       = 4
)

const flagNoReplyExpected = 0x1

// Variant is a decoded variant value.
type Variant struct {
	Sig   string
	Value any
}

// ObjectPath marks a decoded object path.
type ObjectPath string

// Message is a D-Bus message.
type Message struct {
	Type        byte
	Flags       byte
	Serial      uint32
	Path        string
	Interface   string
	Member      string
	ErrorName   string
	ReplySerial uint32
	Destination string
	Sender      string
	Signature   string
	Body        []any
	rawBody     []byte
}

// ---------------------------------------------------------------- encoding

// Encoder writes D-Bus wire data (little endian).
type Encoder struct{ b []byte }

func (e *Encoder) Bytes() []byte { return e.b }

func (e *Encoder) Align(n int) {
	for len(e.b)%n != 0 {
		e.b = append(e.b, 0)
	}
}
func (e *Encoder) Byte(v byte) { e.b = append(e.b, v) }
func (e *Encoder) Bool(v bool) {
	if v {
		e.Uint32(1)
	} else {
		e.Uint32(0)
	}
}
func (e *Encoder) Int32(v int32) { e.Uint32(uint32(v)) }
func (e *Encoder) Uint32(v uint32) {
	e.Align(4)
	e.b = binary.LittleEndian.AppendUint32(e.b, v)
}
func (e *Encoder) String(s string) {
	e.Uint32(uint32(len(s)))
	e.b = append(e.b, s...)
	e.b = append(e.b, 0)
}
func (e *Encoder) ObjectPath(s string) { e.String(s) }
func (e *Encoder) Signature(s string) {
	e.b = append(e.b, byte(len(s)))
	e.b = append(e.b, s...)
	e.b = append(e.b, 0)
}

// Array writes an array whose elements have alignment elemAlign; fill
// writes the elements.
func (e *Encoder) Array(elemAlign int, fill func()) {
	e.Align(4)
	lenPos := len(e.b)
	e.b = append(e.b, 0, 0, 0, 0)
	e.Align(elemAlign)
	start := len(e.b)
	fill()
	binary.LittleEndian.PutUint32(e.b[lenPos:], uint32(len(e.b)-start))
}

// Struct writes a struct or dict entry (8-byte aligned).
func (e *Encoder) Struct(fill func()) {
	e.Align(8)
	fill()
}

// Variant writes a variant of signature sig.
func (e *Encoder) Variant(sig string, fill func()) {
	e.Signature(sig)
	fill()
}

// ---------------------------------------------------------------- decoding

type decoder struct {
	b   []byte
	off int
}

var errShort = errors.New("dbus: message too short")

func (d *decoder) align(n int) error {
	for d.off%n != 0 {
		if d.off >= len(d.b) {
			return errShort
		}
		d.off++
	}
	return nil
}

func (d *decoder) need(n int) error {
	if n < 0 || d.off+n > len(d.b) {
		return errShort
	}
	return nil
}

func (d *decoder) u32() (uint32, error) {
	if err := d.align(4); err != nil {
		return 0, err
	}
	if err := d.need(4); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint32(d.b[d.off:])
	d.off += 4
	return v, nil
}

// splitSig returns the first complete type of a signature and the rest.
func splitSig(sig string) (string, string, error) {
	if sig == "" {
		return "", "", errors.New("dbus: empty signature")
	}
	switch sig[0] {
	case 'a':
		t, rest, err := splitSig(sig[1:])
		return "a" + t, rest, err
	case '(', '{':
		closeCh := byte(')')
		if sig[0] == '{' {
			closeCh = '}'
		}
		depth := 0
		for i := 0; i < len(sig); i++ {
			switch sig[i] {
			case '(', '{':
				depth++
			case ')', '}':
				depth--
				if depth == 0 {
					if sig[i] != closeCh {
						return "", "", errors.New("dbus: bad signature")
					}
					return sig[:i+1], sig[i+1:], nil
				}
			}
		}
		return "", "", errors.New("dbus: unbalanced signature")
	}
	return sig[:1], sig[1:], nil
}

func alignOf(t byte) int {
	switch t {
	case 'y', 'g', 'v':
		return 1
	case 'n', 'q':
		return 2
	case 'x', 't', 'd', '(', '{':
		return 8
	}
	return 4
}

const maxDepth = 32

func (d *decoder) value(sig string, depth int) (any, error) {
	if depth > maxDepth {
		return nil, errors.New("dbus: nesting too deep")
	}
	switch sig[0] {
	case 'y':
		if err := d.need(1); err != nil {
			return nil, err
		}
		v := d.b[d.off]
		d.off++
		return v, nil
	case 'b':
		v, err := d.u32()
		return v != 0, err
	case 'n', 'q':
		if err := d.align(2); err != nil {
			return nil, err
		}
		if err := d.need(2); err != nil {
			return nil, err
		}
		v := binary.LittleEndian.Uint16(d.b[d.off:])
		d.off += 2
		if sig[0] == 'n' {
			return int16(v), nil
		}
		return v, nil
	case 'i':
		v, err := d.u32()
		return int32(v), err
	case 'u', 'h':
		return d.u32()
	case 'x', 't', 'd':
		if err := d.align(8); err != nil {
			return nil, err
		}
		if err := d.need(8); err != nil {
			return nil, err
		}
		v := binary.LittleEndian.Uint64(d.b[d.off:])
		d.off += 8
		return v, nil
	case 's', 'o':
		n, err := d.u32()
		if err != nil {
			return nil, err
		}
		if err := d.need(int(n) + 1); err != nil {
			return nil, err
		}
		s := string(d.b[d.off : d.off+int(n)])
		d.off += int(n) + 1
		if sig[0] == 'o' {
			return ObjectPath(s), nil
		}
		return s, nil
	case 'g':
		if err := d.need(1); err != nil {
			return nil, err
		}
		n := int(d.b[d.off])
		d.off++
		if err := d.need(n + 1); err != nil {
			return nil, err
		}
		s := string(d.b[d.off : d.off+n])
		d.off += n + 1
		return s, nil
	case 'v':
		s, err := d.value("g", depth+1)
		if err != nil {
			return nil, err
		}
		vs := s.(string)
		if vs == "" {
			return nil, errors.New("dbus: empty variant signature")
		}
		t, rest, err := splitSig(vs)
		if err != nil || rest != "" {
			return nil, errors.New("dbus: bad variant signature")
		}
		v, err := d.value(t, depth+1)
		return Variant{Sig: t, Value: v}, err
	case 'a':
		n, err := d.u32()
		if err != nil {
			return nil, err
		}
		if n > 1<<20 {
			return nil, errors.New("dbus: array too large")
		}
		elem := sig[1:]
		if err := d.align(alignOf(elem[0])); err != nil {
			return nil, err
		}
		end := d.off + int(n)
		if end > len(d.b) {
			return nil, errShort
		}
		var out []any
		for d.off < end {
			v, err := d.value(elem, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case '(', '{':
		if err := d.align(8); err != nil {
			return nil, err
		}
		inner := sig[1 : len(sig)-1]
		var out []any
		for inner != "" {
			t, rest, err := splitSig(inner)
			if err != nil {
				return nil, err
			}
			v, err := d.value(t, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			inner = rest
		}
		return out, nil
	}
	return nil, fmt.Errorf("dbus: unsupported type %q", sig[0])
}

func decodeBody(b []byte, sig string, start int) ([]any, error) {
	// Body alignment is relative to the start of the body (which is
	// itself 8-aligned in the message), so offsets work from 0.
	d := &decoder{b: b}
	var out []any
	for sig != "" {
		t, rest, err := splitSig(sig)
		if err != nil {
			return nil, err
		}
		v, err := d.value(t, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		sig = rest
	}
	return out, nil
}

// ---------------------------------------------------------------- messages

func (m *Message) marshal() []byte {
	var h Encoder
	h.Byte('l')
	h.Byte(m.Type)
	h.Byte(m.Flags)
	h.Byte(1)
	h.Uint32(uint32(len(m.rawBody)))
	h.Uint32(m.Serial)
	field := func(code byte, sig string, fill func()) {
		h.Struct(func() {
			h.Byte(code)
			h.Variant(sig, fill)
		})
	}
	h.Array(8, func() {
		if m.Path != "" {
			field(1, "o", func() { h.ObjectPath(m.Path) })
		}
		if m.Interface != "" {
			field(2, "s", func() { h.String(m.Interface) })
		}
		if m.Member != "" {
			field(3, "s", func() { h.String(m.Member) })
		}
		if m.ErrorName != "" {
			field(4, "s", func() { h.String(m.ErrorName) })
		}
		if m.ReplySerial != 0 {
			field(5, "u", func() { h.Uint32(m.ReplySerial) })
		}
		if m.Destination != "" {
			field(6, "s", func() { h.String(m.Destination) })
		}
		if m.Signature != "" {
			field(8, "g", func() { h.Signature(m.Signature) })
		}
	})
	h.Align(8)
	return append(h.b, m.rawBody...)
}

const maxMessage = 1 << 20

func readMessage(r *bufio.Reader) (*Message, error) {
	fixed := make([]byte, 16)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return nil, err
	}
	if fixed[0] != 'l' {
		return nil, errors.New("dbus: big-endian messages are not supported")
	}
	bodyLen := binary.LittleEndian.Uint32(fixed[4:])
	fieldsLen := binary.LittleEndian.Uint32(fixed[12:])
	if bodyLen > maxMessage || fieldsLen > maxMessage {
		return nil, errors.New("dbus: message too large")
	}
	hdrEnd := 16 + int(fieldsLen)
	pad := (8 - hdrEnd%8) % 8
	rest := make([]byte, int(fieldsLen)+pad+int(bodyLen))
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}
	m := &Message{Type: fixed[1], Flags: fixed[2], Serial: binary.LittleEndian.Uint32(fixed[8:])}
	// Header fields: decode the array a(yv) from the full header bytes so
	// that alignment matches.
	full := append(append([]byte{}, fixed...), rest[:fieldsLen]...)
	d := &decoder{b: full, off: 12}
	v, err := d.value("a(yv)", 0)
	if err != nil {
		return nil, err
	}
	for _, f := range v.([]any) {
		st := f.([]any)
		code := st[0].(byte)
		val := st[1].(Variant).Value
		switch code {
		case 1:
			if p, ok := val.(ObjectPath); ok {
				m.Path = string(p)
			}
		case 2:
			m.Interface, _ = val.(string)
		case 3:
			m.Member, _ = val.(string)
		case 4:
			m.ErrorName, _ = val.(string)
		case 5:
			m.ReplySerial, _ = val.(uint32)
		case 6:
			m.Destination, _ = val.(string)
		case 7:
			m.Sender, _ = val.(string)
		case 8:
			m.Signature, _ = val.(string)
		}
	}
	m.rawBody = rest[int(fieldsLen)+pad:]
	if m.Signature != "" {
		if m.Body, err = decodeBody(m.rawBody, m.Signature, 0); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// ---------------------------------------------------------------- connection

// Handler answers method calls on an exported object. It returns the reply
// signature and body, or an error name.
type Handler func(m *Message) (sig string, body []byte, errName string)

// Conn is a connection to a message bus.
type Conn struct {
	c      net.Conn
	r      *bufio.Reader
	wmu    sync.Mutex
	mu     sync.Mutex
	serial uint32
	calls  map[uint32]chan *Message
	paths  map[string]Handler
	signal func(*Message)
	name   string
	closed chan struct{}
}

// SessionBus connects to the user's session bus.
func SessionBus() (*Conn, error) {
	addr := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if addr == "" {
		if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
			addr = "unix:path=" + rt + "/bus"
		} else {
			return nil, errors.New("dbus: no session bus")
		}
	}
	return Dial(addr)
}

// Dial connects to a bus address such as "unix:path=/run/user/1000/bus".
func Dial(addr string) (*Conn, error) {
	var nc net.Conn
	var err error
	for _, a := range strings.Split(addr, ";") {
		if !strings.HasPrefix(a, "unix:") {
			continue
		}
		params := map[string]string{}
		for _, kv := range strings.Split(a[5:], ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				params[k] = v
			}
		}
		switch {
		case params["path"] != "":
			nc, err = net.DialTimeout("unix", params["path"], 2*time.Second)
		case params["abstract"] != "":
			nc, err = net.DialTimeout("unix", "@"+params["abstract"], 2*time.Second)
		default:
			continue
		}
		if err == nil {
			break
		}
	}
	if nc == nil {
		if err == nil {
			err = errors.New("dbus: unsupported bus address")
		}
		return nil, err
	}
	c := &Conn{c: nc, r: bufio.NewReader(nc), calls: map[uint32]chan *Message{}, paths: map[string]Handler{}, closed: make(chan struct{})}
	if err := c.auth(); err != nil {
		nc.Close()
		return nil, err
	}
	go c.readLoop()
	reply, err := c.Call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "Hello", "", nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	if len(reply.Body) == 1 {
		c.name, _ = reply.Body[0].(string)
	}
	return c, nil
}

func (c *Conn) auth() error {
	c.c.SetDeadline(time.Now().Add(5 * time.Second))
	defer c.c.SetDeadline(time.Time{})
	uid := strconv.Itoa(os.Getuid())
	hexUID := fmt.Sprintf("%x", uid)
	if _, err := fmt.Fprintf(c.c, "\x00AUTH EXTERNAL %s\r\n", hexUID); err != nil {
		return err
	}
	line, err := c.r.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, "OK") {
		return fmt.Errorf("dbus: authentication refused: %s", strings.TrimSpace(line))
	}
	_, err = io.WriteString(c.c, "BEGIN\r\n")
	return err
}

// UniqueName returns the connection's unique bus name.
func (c *Conn) UniqueName() string { return c.name }

// Close closes the connection.
func (c *Conn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return c.c.Close()
}

// Done is closed when the connection ends.
func (c *Conn) Done() <-chan struct{} { return c.closed }

func (c *Conn) nextSerial() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.serial++
	return c.serial
}

func (c *Conn) send(m *Message) error {
	if m.Serial == 0 {
		m.Serial = c.nextSerial()
	}
	b := m.marshal()
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.c.Write(b)
	return err
}

// Call calls a method and waits (up to 5 s) for the reply.
func (c *Conn) Call(dest, path, iface, member, sig string, body []byte) (*Message, error) {
	m := &Message{Type: TypeMethodCall, Destination: dest, Path: path, Interface: iface, Member: member, Signature: sig, rawBody: body}
	m.Serial = c.nextSerial()
	ch := make(chan *Message, 1)
	c.mu.Lock()
	c.calls[m.Serial] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.calls, m.Serial); c.mu.Unlock() }()
	if err := c.send(m); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Type == TypeError {
			msg := ""
			if len(r.Body) > 0 {
				msg, _ = r.Body[0].(string)
			}
			return r, fmt.Errorf("%s: %s", r.ErrorName, msg)
		}
		return r, nil
	case <-time.After(5 * time.Second):
		return nil, errors.New("dbus: call timed out")
	case <-c.closed:
		return nil, errors.New("dbus: connection closed")
	}
}

// Emit sends a signal.
func (c *Conn) Emit(path, iface, member, sig string, body []byte) error {
	return c.send(&Message{Type: TypeSignal, Path: path, Interface: iface, Member: member, Signature: sig, rawBody: body, Flags: flagNoReplyExpected})
}

// Export answers method calls made on path.
func (c *Conn) Export(path string, h Handler) {
	c.mu.Lock()
	c.paths[path] = h
	c.mu.Unlock()
}

// OnSignal registers the signal callback.
func (c *Conn) OnSignal(fn func(*Message)) {
	c.mu.Lock()
	c.signal = fn
	c.mu.Unlock()
}

func (c *Conn) readLoop() {
	defer c.Close()
	for {
		m, err := readMessage(c.r)
		if err != nil {
			return
		}
		switch m.Type {
		case TypeMethodReturn, TypeError:
			c.mu.Lock()
			ch := c.calls[m.ReplySerial]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case TypeSignal:
			c.mu.Lock()
			fn := c.signal
			c.mu.Unlock()
			if fn != nil {
				go fn(m)
			}
		case TypeMethodCall:
			go c.answer(m)
		}
	}
}

func (c *Conn) answer(m *Message) {
	reply := &Message{Type: TypeMethodReturn, ReplySerial: m.Serial, Destination: m.Sender}
	if m.Interface == "org.freedesktop.DBus.Peer" && m.Member == "Ping" {
		c.send(reply)
		return
	}
	c.mu.Lock()
	h := c.paths[m.Path]
	c.mu.Unlock()
	var sig, errName string
	var body []byte
	if h == nil {
		errName = "org.freedesktop.DBus.Error.UnknownObject"
	} else {
		sig, body, errName = h(m)
	}
	if m.Flags&flagNoReplyExpected != 0 {
		return
	}
	if errName != "" {
		var e Encoder
		e.String(errName)
		reply.Type, reply.ErrorName, reply.Signature, reply.rawBody = TypeError, errName, "s", e.b
	} else {
		reply.Signature, reply.rawBody = sig, body
	}
	c.send(reply)
}
