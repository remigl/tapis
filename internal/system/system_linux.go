//go:build linux

package system

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var errUnknown = errors.New("unknown action")

func run(cmds ...[]string) error {
	var errs []string
	for _, c := range cmds {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
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
