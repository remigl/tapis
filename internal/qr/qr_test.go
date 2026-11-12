package qr

import (
	"strings"
	"testing"
)

func TestVersions(t *testing.T) {
	for n, size := range map[int]int{1: 21, 26: 25, 27: 29, 100: 41, 213: 57} {
		c, err := Encode(strings.Repeat("a", n))
		if err != nil {
			t.Fatal(err)
		}
		if c.Size != size {
			t.Errorf("len %d: size %d, want %d", n, c.Size, size)
		}
	}
	if _, err := Encode(strings.Repeat("a", 214)); err != ErrTooLong {
		t.Error("expected ErrTooLong")
	}
}
