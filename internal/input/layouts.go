package input

import (
	"fmt"
	"sort"
	"strings"
)

// Keyboard layouts for the Linux backend.
//
// uinput sends physical key codes, and the desktop translates them with the
// active keyboard layout. To type a character we therefore need to know which
// physical key (and which modifier level) produces it on the user's layout.
// These tables mirror the XKB definitions (symbols/us, fr, be). Characters
// that are not reachable fall back to the Ctrl+Shift+U Unicode input method.
//
// Each key lists up to four levels: plain, Shift, AltGr, Shift+AltGr.
// A token "dead:X" marks a dead key (X is one of ^ ¨ ` ´ ~).

type stroke struct {
	code  uint16 // evdev key code
	level uint8  // 1..4
}

// Layout maps characters to key strokes.
type Layout struct {
	Name  string
	chars map[rune][]stroke
}

// evdev codes of the XKB key names used below.
var xkbCodes = map[string]uint16{"TLDE": 41, "BKSL": 43, "LSGT": 86, "SPCE": 57}

func init() {
	for i := 1; i <= 12; i++ {
		xkbCodes[fmt.Sprintf("AE%02d", i)] = uint16(1 + i)
		xkbCodes[fmt.Sprintf("AD%02d", i)] = uint16(15 + i)
	}
	for i := 1; i <= 11; i++ {
		xkbCodes[fmt.Sprintf("AC%02d", i)] = uint16(29 + i)
	}
	for i := 1; i <= 10; i++ {
		xkbCodes[fmt.Sprintf("AB%02d", i)] = uint16(43 + i)
	}
}

const latinLetters = `AD01 q Q|AD02 w W|AD03 e E|AD04 r R ¶ ®|AD05 t T ŧ Ŧ|AD06 y Y ← ¥|AD07 u U ↓ ↑|AD08 i I → ı|
AD09 o O ø Ø|AD10 p P þ Þ|AC01 a A æ Æ|AC02 s S ß ẞ|AC03 d D ð Ð|AC04 f F đ ª|AC05 g G ŋ Ŋ|AC06 h H ħ Ħ|
AC07 j J|AC08 k K ĸ &|AC09 l L ł Ł|AB01 z Z « <|AB02 x X » >|AB03 c C ¢ ©|AB04 v V „ ‚|AB05 b B “ ‘|
AB06 n N ” ’|AB07 m M µ º|SPCE SPACE`

// Each definition is applied on top of latinLetters (like XKB's include "latin").
var layoutDefs = map[string]string{
	"us": `TLDE ` + "`" + ` ~|AE01 1 !|AE02 2 @|AE03 3 #|AE04 4 $|AE05 5 %|AE06 6 ^|AE07 7 &|AE08 8 *|AE09 9 (|AE10 0 )|
AE11 - _|AE12 = +|AD01 q Q|AD02 w W|AD03 e E|AD11 [ {|AD12 ] }|AC01 a A|AC10 ; :|AC11 ' "|BKSL \ PIPE|
AB01 z Z|AB02 x X|AB07 m M|AB08 , <|AB09 . >|AB10 / ?`,

	"fr": `TLDE ² ~ ¬|AE01 & 1 ¹ ¡|AE02 é 2 ~ ⅛|AE03 " 3 # £|AE04 ' 4 { $|AE05 ( 5 [ ⅜|AE06 - 6 PIPE ⅝|
AE07 è 7 ` + "`" + ` ⅞|AE08 _ 8 \ ™|AE09 ç 9 ^ ±|AE10 à 0 @ °|AE11 ) ° ] ¿|AE12 = + }|
AD01 a A æ Æ|AD02 z Z « <|AD03 e E € ¢|AD11 dead:^ dead:¨|AD12 $ £ ¤|
AC01 q Q @ Ω|AC10 m M µ º|AC11 ù % dead:^|BKSL * µ dead:` + "`" + `|
AB01 w W ł Ł|AB07 , ? dead:´|AB08 ; . • ×|AB09 : / · ÷|AB10 ! §|LSGT < > PIPE ¦`,

	"be": `TLDE ² ³ ¬|AE01 & 1 PIPE ¡|AE02 é 2 @ ⅛|AE03 " 3 # £|AE04 ' 4 ¼ $|AE05 ( 5 ½ ⅜|AE06 § 6 ^ ⅝|
AE07 è 7 { ⅞|AE08 ! 8 [ ™|AE09 ç 9 { ±|AE10 à 0 } °|AE11 ) ° \ ¿|AE12 - _|
AD01 a A @ Ω|AD02 z Z ſ §|AD03 e E € ¢|AD09 o O œ Œ|AD11 dead:^ dead:¨ [|AD12 $ * ]|
AC01 q Q æ Æ|AC10 m M dead:´|AC11 ù % dead:´|BKSL µ £ dead:` + "`" + `|
LSGT < > \ \|AB01 w W « <|AB07 , ? NONE º|AB08 ; . • ×|AB09 : / · ÷|AB10 = + dead:~`,
}

// Dead key compositions (what the default XKB Compose table produces).
var deadCompose = map[rune]map[rune]rune{
	'^': pairs("aâeêiîoôuûAÂEÊIÎOÔUÛ ^"),
	'¨': pairs("aäeëiïoöuüyÿAÄEËIÏOÖUÜYŸ"),
	'`': pairs("aàeèiìoòuùAÀEÈIÌOÒUÙ `"),
	'´': pairs("aáeéiíoóuúyýAÁEÉIÍOÓUÚYÝ"),
	'~': pairs("aãnñoõAÃNÑOÕ ~"),
}

func pairs(s string) map[rune]rune {
	r := []rune(s)
	m := map[rune]rune{}
	for i := 0; i+1 < len(r); i += 2 {
		m[r[i]] = r[i+1]
	}
	return m
}

type keyDef struct {
	code   uint16
	levels []string
}

func parseDefs(s string) map[uint16][]string {
	out := map[uint16][]string{}
	for _, entry := range strings.Split(s, "|") {
		f := strings.Fields(entry)
		if len(f) < 2 {
			continue
		}
		code, ok := xkbCodes[f[0]]
		if !ok {
			panic("unknown xkb key " + f[0])
		}
		out[code] = f[1:]
	}
	return out
}

// LayoutNames lists the built-in layouts.
func LayoutNames() []string {
	var n []string
	for k := range layoutDefs {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// LoadLayout builds the character table for a built-in layout.
func LoadLayout(name string) (*Layout, error) {
	def, ok := layoutDefs[name]
	if !ok {
		return nil, fmt.Errorf("unknown keyboard layout %q (available: %s)", name, strings.Join(LayoutNames(), ", "))
	}
	keys := parseDefs(latinLetters)
	for code, lv := range parseDefs(def) {
		keys[code] = lv
	}
	if name == "us" { // the US layout has no AltGr level
		for c, lv := range keys {
			if len(lv) > 2 {
				keys[c] = lv[:2]
			}
		}
	}
	codes := make([]int, 0, len(keys))
	for c := range keys {
		codes = append(codes, int(c))
	}
	sort.Ints(codes)

	l := &Layout{Name: name, chars: map[rune][]stroke{}}
	deadKeys := map[rune]stroke{}
	// Lower levels win: walk level by level.
	for level := 1; level <= 4; level++ {
		for _, c := range codes {
			lv := keys[uint16(c)]
			if level > len(lv) {
				continue
			}
			tok := lv[level-1]
			st := stroke{uint16(c), uint8(level)}
			switch {
			case tok == "NONE":
				continue
			case tok == "SPACE":
				tok = " "
			case tok == "PIPE":
				tok = "|"
			case strings.HasPrefix(tok, "dead:"):
				d := []rune(strings.TrimPrefix(tok, "dead:"))[0]
				if _, seen := deadKeys[d]; !seen {
					deadKeys[d] = st
				}
				continue
			}
			r := []rune(tok)
			if len(r) != 1 {
				panic("bad layout token " + tok)
			}
			if _, seen := l.chars[r[0]]; !seen {
				l.chars[r[0]] = []stroke{st}
			}
		}
	}
	// Characters reachable through a dead key followed by a base key.
	for d, st := range deadKeys {
		for base, out := range deadCompose[d] {
			if _, seen := l.chars[out]; seen {
				continue
			}
			if b, ok := l.chars[base]; ok && len(b) == 1 {
				l.chars[out] = []stroke{st, b[0]}
			}
		}
	}
	return l, nil
}

// Lookup returns the strokes needed to type r, or nil.
func (l *Layout) Lookup(r rune) []stroke { return l.chars[r] }
