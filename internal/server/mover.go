package server

import (
	"math"
	"sync"
	"time"

	"github.com/remigl/tapis/internal/input"
)

// mover smooths pointer motion. Phones report touch movement at the screen
// refresh rate (60–120 Hz) and Wi-Fi delivers it in bursts, which looks
// jerky on a 144 Hz monitor. Instead of jumping by each received delta,
// the mover releases pending motion in small steps every 4 ms, roughly
// half of what is left at each step. This adds about 5 ms of latency and
// makes the pointer glide.
type mover struct {
	in      input.Injector
	mu      sync.Mutex
	px, py  float64 // motion not yet sent
	running bool
	smooth  bool
}

const (
	moverTick     = 4 * time.Millisecond
	moverFraction = 0.5
)

func newMover(in input.Injector, smooth bool) *mover { return &mover{in: in, smooth: smooth} }

// Add queues relative motion.
func (m *mover) Add(dx, dy int) error {
	if !m.smooth {
		return m.in.Move(dx, dy)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.px += float64(dx)
	m.py += float64(dy)
	if !m.running {
		m.running = true
		go m.loop()
	}
	return nil
}

func (m *mover) loop() {
	t := time.NewTicker(moverTick)
	defer t.Stop()
	for range t.C {
		m.mu.Lock()
		sx, sy := step(m.px), step(m.py)
		m.px -= float64(sx)
		m.py -= float64(sy)
		done := sx == 0 && sy == 0
		if done {
			m.running = false
		}
		m.mu.Unlock()
		if done {
			return
		}
		m.in.Move(sx, sy)
	}
}

// step returns how many whole pixels to send now out of p pending ones.
func step(p float64) int {
	if math.Abs(p) < 1 {
		return 0
	}
	s := p * moverFraction
	if math.Abs(s) < 1 {
		return int(math.Copysign(1, p))
	}
	return int(math.Round(s))
}

// Flush sends all pending motion right away, so that a click lands exactly
// where the pointer was heading.
func (m *mover) Flush() {
	if !m.smooth {
		return
	}
	m.mu.Lock()
	sx, sy := int(math.Round(m.px)), int(math.Round(m.py))
	m.px, m.py = 0, 0
	m.mu.Unlock()
	if sx != 0 || sy != 0 {
		m.in.Move(sx, sy)
	}
}
