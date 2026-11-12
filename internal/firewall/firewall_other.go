//go:build !windows

package firewall

// Ensure is a no-op outside Windows: most Linux desktops do not filter
// incoming connections by default (see TROUBLESHOOTING.md for ufw/firewalld).
func Ensure(port int, announce func()) (Status, error) { return NotApplicable, nil }
