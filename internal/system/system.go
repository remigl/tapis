// Package system performs session-level actions: locking the screen and
// putting the computer to sleep. Commands are fixed; nothing from the
// network is ever passed to them.
package system

// Action is a system action requested from the phone.
type Action string

const (
	Lock  Action = "lock"
	Sleep Action = "sleep"
)

// Valid reports whether a is a known action.
func Valid(a Action) bool { return a == Lock || a == Sleep }

// Do performs the action.
func Do(a Action) error {
	switch a {
	case Lock:
		return lock()
	case Sleep:
		return sleep()
	}
	return errUnknown
}
