// Package lockscreen lets Tapis type on the Windows lock screen (to enter
// a PIN or password from the phone).
//
// Windows shows the lock screen on a separate, secure desktop. Programs of
// the user cannot send input there; only a process running as SYSTEM on
// that desktop can. Tapis therefore uses an optional helper:
//
//	Tapis (user, network)  ──named pipe──►  agent (SYSTEM, lock-screen desktop)
//	                                         ▲ started in the user's session by
//	                                         │ the helper task (SYSTEM, at boot)
//
// Safety rules, enforced by the agent:
//   - it only injects input while the session is LOCKED, so it can never be
//     used to approve an administrator (UAC) prompt;
//   - its pipe accepts only SYSTEM and the user logged on in that session,
//     and refuses remote clients;
//   - Tapis checks that the pipe really belongs to a SYSTEM process before
//     sending anything (no impersonation by another program);
//   - the helper runs from Program Files, which only administrators can
//     modify, never from the user's Downloads folder;
//   - the agent never touches the network.
//
// The helper is opt-in (menu "Type on the lock screen") and can be removed
// the same way.
package lockscreen

// event is one message on the pipe (one JSON object per line).
type event struct {
	Op   string `json:"op"` // move, scroll, button, key, text
	X    int    `json:"x,omitempty"`
	Y    int    `json:"y,omitempty"`
	B    int    `json:"b,omitempty"`
	Down bool   `json:"down,omitempty"`
	K    string `json:"k,omitempty"`
	S    string `json:"s,omitempty"`
}

// TaskName is the name of the scheduled task that starts the helper.
const TaskName = "Tapis lock screen helper"
