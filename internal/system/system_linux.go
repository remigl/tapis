//go:build linux

package system

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var errUnknown = errors.New("unknown action")

// bin returns the absolute path of a system command, preferring the
// standard system directories over the user's PATH.
func bin(name string) string {
	for _, d := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		if p := d + "/" + name; isExec(p) {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

func run(cmds ...[]string) error {
	var errs []string
	for _, c := range cmds {
		path := bin(c[0])
		if path == "" {
			continue
		}
		out, err := exec.Command(path, c[1:]...).CombinedOutput()
		if err == nil {
			return nil
		}
		errs = append(errs, strings.Join(c, " ")+": "+strings.TrimSpace(string(out)))
	}
	if len(errs) == 0 {
		return fmt.Errorf("no suitable command found (%s)", cmds[0][0])
	}
	return errors.New(strings.Join(errs, "; "))
}

func lock() error {
	return run(
		[]string{"loginctl", "lock-session"},
		[]string{"loginctl", "lock-sessions"},
		[]string{"xdg-screensaver", "lock"},
	)
}

func sleep() error { return run([]string{"systemctl", "suspend"}) }
