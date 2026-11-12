package server

import (
	"sync"
	"testing"
	"time"

	"github.com/remigl/tapis/internal/input"
)

type sumInjector struct {
	input.Logger
	mu     sync.Mutex
	x, y   int
	events int
}

func (s *sumInjector) Move(dx, dy int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.x += dx
	s.y += dy
	s.events++
	return nil
}

func TestMoverKeepsTotalMotion(t *testing.T) {
	in := &sumInjector{}
	m := newMover(in, true)
	for i := 0; i < 20; i++ {
		m.Add(7, -3)
		time.Sleep(16 * time.Millisecond)
	}
	m.Add(1, 0)
	time.Sleep(100 * time.Millisecond)
	m.Flush()
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.x != 141 || in.y != -60 {
		t.Fatalf("total motion %d,%d, want 141,-60", in.x, in.y)
	}
	if in.events <= 21 {
		t.Fatalf("expected motion to be split in more steps, got %d", in.events)
	}
}
