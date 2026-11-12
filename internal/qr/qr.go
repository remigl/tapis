// Package qr is a small, dependency-free QR Code encoder (byte mode,
// error-correction level M, versions 1–10). It is just enough to encode
// the pairing URL that Tapis shows on the computer screen.
package qr

import (
	"errors"
	"image"
	"image/color"
	"strings"
)

// Code is an encoded QR symbol. Modules[y][x] is true for a dark module.
type Code struct {
	Size    int
	Modules [][]bool
}

type versionInfo struct {
	total, ecPerBlock, blocks int
	align                     []int
}

// Level M parameters for versions 1..10 (ISO/IEC 18004 table 9).
var versions = []versionInfo{
	{},
	{26, 10, 1, nil},
	{44, 16, 1, []int{6, 18}},
	{70, 26, 1, []int{6, 22}},
	{100, 18, 2, []int{6, 26}},
	{134, 24, 2, []int{6, 30}},
	{172, 16, 4, []int{6, 34}},
	{196, 18, 4, []int{6, 22, 38}},
	{242, 22, 4, []int{6, 24, 42}},
	{292, 22, 5, []int{6, 26, 46}},
	{346, 26, 5, []int{6, 28, 50}},
}

// ErrTooLong is returned when the data does not fit in version 10-M.
var ErrTooLong = errors.New("qr: data too long")

// Encode encodes data as a QR Code, choosing the smallest version.
func Encode(data string) (*Code, error) {
	b := []byte(data)
	for v := 1; v <= 10; v++ {
		vi := versions[v]
		dataCap := vi.total - vi.ecPerBlock*vi.blocks
		countBits := 8
		if v >= 10 {
			countBits = 16
		}
		if 4+countBits+8*len(b) <= dataCap*8 {
			return build(v, b, countBits, dataCap), nil
		}
	}
	return nil, ErrTooLong
}

type bitBuf struct{ bits []bool }

func (bb *bitBuf) put(val uint, n int) {
	for i := n - 1; i >= 0; i-- {
		bb.bits = append(bb.bits, (val>>uint(i))&1 == 1)
	}
}

func build(ver int, data []byte, countBits, dataCap int) *Code {
	vi := versions[ver]
	var bb bitBuf
	bb.put(0x4, 4) // byte mode
	bb.put(uint(len(data)), countBits)
	for _, c := range data {
		bb.put(uint(c), 8)
	}
	capBits := dataCap * 8
	for i := 0; i < 4 && len(bb.bits) < capBits; i++ {
		bb.bits = append(bb.bits, false)
	}
	for len(bb.bits)%8 != 0 {
		bb.bits = append(bb.bits, false)
	}
	for pad := byte(0xEC); len(bb.bits) < capBits; pad ^= 0xEC ^ 0x11 {
		bb.put(uint(pad), 8)
	}
	cw := make([]byte, dataCap)
	for i := range cw {
		for j := 0; j < 8; j++ {
			if bb.bits[i*8+j] {
				cw[i] |= 1 << uint(7-j)
			}
		}
	}

	// Split into blocks, compute Reed-Solomon EC, interleave.
	nShort := vi.blocks - dataCap%vi.blocks
	shortLen := dataCap / vi.blocks
	div := rsDivisor(vi.ecPerBlock)
	var dBlocks, eBlocks [][]byte
	k := 0
	for i := 0; i < vi.blocks; i++ {
		n := shortLen
		if i >= nShort {
			n++
		}
		blk := cw[k : k+n]
		k += n
		dBlocks = append(dBlocks, blk)
		eBlocks = append(eBlocks, rsRemainder(blk, div))
	}
	var final []byte
	for i := 0; i <= shortLen; i++ {
		for _, blk := range dBlocks {
			if i < len(blk) {
				final = append(final, blk[i])
			}
		}
	}
	for i := 0; i < vi.ecPerBlock; i++ {
		for _, blk := range eBlocks {
			final = append(final, blk[i])
		}
	}

	m := newMatrix(ver)
	m.drawFunctionPatterns()
	m.drawCodewords(final)

	best, bestPenalty := -1, int(^uint(0)>>1)
	for mask := 0; mask < 8; mask++ {
		m.applyMask(mask)
		m.drawFormat(mask)
		if p := m.penalty(); p < bestPenalty {
			best, bestPenalty = mask, p
		}
		m.applyMask(mask) // XOR again to undo
	}
	m.applyMask(best)
	m.drawFormat(best)
	return &Code{Size: m.size, Modules: m.mod}
}

// ---- matrix ----

type matrix struct {
	ver, size int
	mod, fn   [][]bool
}

func newMatrix(ver int) *matrix {
	s := ver*4 + 17
	m := &matrix{ver: ver, size: s}
	m.mod = make([][]bool, s)
	m.fn = make([][]bool, s)
	for i := range m.mod {
		m.mod[i] = make([]bool, s)
		m.fn[i] = make([]bool, s)
	}
	return m
}

func (m *matrix) set(x, y int, dark bool) {
	m.mod[y][x] = dark
	m.fn[y][x] = true
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func (m *matrix) drawFunctionPatterns() {
	for i := 0; i < m.size; i++ {
		m.set(6, i, i%2 == 0)
		m.set(i, 6, i%2 == 0)
	}
	for _, c := range [][2]int{{3, 3}, {m.size - 4, 3}, {3, m.size - 4}} {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := c[0]+dx, c[1]+dy
				if x < 0 || y < 0 || x >= m.size || y >= m.size {
					continue
				}
				d := max(abs(dx), abs(dy))
				m.set(x, y, d != 2 && d != 4)
			}
		}
	}
	al := versions[m.ver].align
	n := len(al)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if (i == 0 && j == 0) || (i == 0 && j == n-1) || (i == n-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					m.set(al[i]+dx, al[j]+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	m.drawFormat(0) // reserve the area; real bits drawn later
	if m.ver >= 7 {
		rem := m.ver
		for i := 0; i < 12; i++ {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
		}
		bits := m.ver<<12 | rem
		for i := 0; i < 18; i++ {
			bit := (bits>>uint(i))&1 == 1
			a, b := m.size-11+i%3, i/3
			m.set(a, b, bit)
			m.set(b, a, bit)
		}
	}
}

func (m *matrix) drawFormat(mask int) {
	data := 0<<3 | mask // level M = 0b00
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return (bits>>uint(i))&1 == 1 }
	for i := 0; i <= 5; i++ {
		m.set(8, i, bit(i))
	}
	m.set(8, 7, bit(6))
	m.set(8, 8, bit(7))
	m.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		m.set(14-i, 8, bit(i))
	}
	for i := 0; i < 8; i++ {
		m.set(m.size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		m.set(8, m.size-15+i, bit(i))
	}
	m.set(8, m.size-8, true)
}

func (m *matrix) drawCodewords(data []byte) {
	i := 0
	for right := m.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < m.size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				y := vert
				if (right+1)&2 == 0 {
					y = m.size - 1 - vert
				}
				if !m.fn[y][x] && i < len(data)*8 {
					m.mod[y][x] = (data[i>>3]>>uint(7-(i&7)))&1 == 1
					i++
				}
			}
		}
	}
}

func (m *matrix) applyMask(mask int) {
	for y := 0; y < m.size; y++ {
		for x := 0; x < m.size; x++ {
			if m.fn[y][x] {
				continue
			}
			var inv bool
			switch mask {
			case 0:
				inv = (x+y)%2 == 0
			case 1:
				inv = y%2 == 0
			case 2:
				inv = x%3 == 0
			case 3:
				inv = (x+y)%3 == 0
			case 4:
				inv = (x/3+y/2)%2 == 0
			case 5:
				inv = x*y%2+x*y%3 == 0
			case 6:
				inv = (x*y%2+x*y%3)%2 == 0
			case 7:
				inv = ((x+y)%2+x*y%3)%2 == 0
			}
			if inv {
				m.mod[y][x] = !m.mod[y][x]
			}
		}
	}
}

func (m *matrix) penalty() int {
	s, p := m.size, 0
	get := func(x, y int, horiz bool) bool {
		if horiz {
			return m.mod[y][x]
		}
		return m.mod[x][y]
	}
	for _, horiz := range []bool{true, false} {
		for a := 0; a < s; a++ {
			run := 1
			for b := 1; b < s; b++ {
				if get(b, a, horiz) == get(b-1, a, horiz) {
					run++
					if run == 5 {
						p += 3
					} else if run > 5 {
						p++
					}
				} else {
					run = 1
				}
			}
			// finder-like pattern 1011101 with 4 light modules on one side
			for b := 0; b+6 < s; b++ {
				if get(b, a, horiz) && !get(b+1, a, horiz) && get(b+2, a, horiz) && get(b+3, a, horiz) &&
					get(b+4, a, horiz) && !get(b+5, a, horiz) && get(b+6, a, horiz) {
					light := func(from, to int) bool {
						for k := from; k < to; k++ {
							if k >= 0 && k < s && get(k, a, horiz) {
								return false
							}
						}
						return true
					}
					if light(b-4, b) || light(b+7, b+11) {
						p += 40
					}
				}
			}
		}
	}
	dark := 0
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			if m.mod[y][x] {
				dark++
			}
			if x+1 < s && y+1 < s {
				c := m.mod[y][x]
				if c == m.mod[y][x+1] && c == m.mod[y+1][x] && c == m.mod[y+1][x+1] {
					p += 3
				}
			}
		}
	}
	total := s * s
	k := (abs(dark*20-total*10) + total - 1) / total
	p += (k - 1) * 10
	if k == 0 {
		p += 10
	}
	return p
}

// ---- Reed-Solomon over GF(256), polynomial 0x11D ----

func gfMul(x, y byte) byte {
	var z int
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x11D)
		z ^= int((y>>uint(i))&1) * int(x)
	}
	return byte(z)
}

func rsDivisor(degree int) []byte {
	res := make([]byte, degree)
	res[degree-1] = 1
	root := byte(1)
	for i := 0; i < degree; i++ {
		for j := range res {
			res[j] = gfMul(res[j], root)
			if j+1 < len(res) {
				res[j] ^= res[j+1]
			}
		}
		root = gfMul(root, 0x02)
	}
	return res
}

func rsRemainder(data, div []byte) []byte {
	res := make([]byte, len(div))
	for _, b := range data {
		factor := b ^ res[0]
		copy(res, res[1:])
		res[len(res)-1] = 0
		for i := range res {
			res[i] ^= gfMul(div[i], factor)
		}
	}
	return res
}

// ---- rendering ----

// Terminal renders the code with Unicode half blocks. Light modules are
// drawn with block characters so it scans on dark terminal backgrounds.
func (c *Code) Terminal() string {
	const quiet = 2
	n := c.Size + 2*quiet
	light := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		if x < 0 || y < 0 || x >= c.Size || y >= c.Size {
			return true
		}
		return !c.Modules[y][x]
	}
	var sb strings.Builder
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			top, bot := light(x, y), y+1 >= n || light(x, y+1)
			if y+1 >= n {
				bot = false
			}
			switch {
			case top && bot:
				sb.WriteRune('█')
			case top:
				sb.WriteRune('▀')
			case bot:
				sb.WriteRune('▄')
			default:
				sb.WriteRune(' ')
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Image renders the code as a black-on-white image with a 4-module quiet zone.
func (c *Code) Image(scale int) image.Image {
	const quiet = 4
	n := (c.Size + 2*quiet) * scale
	img := image.NewGray(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			mx, my := x/scale-quiet, y/scale-quiet
			v := uint8(255)
			if mx >= 0 && my >= 0 && mx < c.Size && my < c.Size && c.Modules[my][mx] {
				v = 0
			}
			img.SetGray(x, y, color.Gray{v})
		}
	}
	return img
}
