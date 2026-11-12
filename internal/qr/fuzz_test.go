package qr

import "testing"

func FuzzEncode(f *testing.F) {
	f.Add("https://192.168.1.10:47800/#pair=abc")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		c, err := Encode(s)
		if err != nil {
			if len(s) <= 213 {
				t.Fatalf("%d bytes should fit", len(s))
			}
			return
		}
		if c.Size < 21 || c.Size > 57 || len(c.Modules) != c.Size {
			t.Fatalf("bad size %d", c.Size)
		}
		_ = c.Terminal()
	})
}
