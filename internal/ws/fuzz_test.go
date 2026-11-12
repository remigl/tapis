package ws

import (
	"bufio"
	"bytes"
	"net"
	"testing"
	"time"
)

// FuzzReadMessage feeds arbitrary bytes to the frame parser: it must never
// panic, hang or return a message larger than MaxMessage.
func FuzzReadMessage(f *testing.F) {
	f.Add(clientFrame(opText, []byte(`{"t":"move","x":1}`)))
	f.Add(append(clientFrame(opPing, []byte("x")), clientFrame(opText, []byte("y"))...))
	f.Add([]byte{0x81, 0xFF, 0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 1, 2, 3, 4})
	f.Add([]byte{0x01, 0x82, 1, 2, 3, 4, 'a', 'b', 0x80, 0x81, 1, 2, 3, 4, 'c'})
	f.Fuzz(func(t *testing.T, data []byte) {
		server, client := net.Pipe()
		defer server.Close()
		go func() {
			client.Write(data)
			client.Close()
		}()
		go func() { // swallow pongs/close frames
			b := make([]byte, 512)
			for {
				if _, err := client.Read(b); err != nil {
					return
				}
			}
		}()
		c := &Conn{c: server, br: bufio.NewReader(server)}
		server.SetDeadline(time.Now().Add(2 * time.Second))
		for i := 0; i < 64; i++ {
			msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if len(msg) > MaxMessage {
				t.Fatalf("message of %d bytes accepted", len(msg))
			}
		}
	})
}

var _ = bytes.MinRead
