// Package focus tells whether the element that has the keyboard focus on
// the computer accepts text (a text box, a search field, a document…), so
// that the phone can open its keyboard automatically.
//
//   - Windows: UI Automation, the accessibility API also used by screen
//     readers and by the Windows touch keyboard.
//   - Other systems: not supported yet (the phone keyboard opens manually).
package focus

// Probe reports whether the focused element accepts text input.
type Probe interface {
	Editable() bool
	Close()
}

// New returns a probe, or nil if the system is not supported.
func New() Probe { return newProbe() }
