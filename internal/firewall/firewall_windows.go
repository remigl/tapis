//go:build windows

package firewall

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

// Windows Firewall applies a different profile to "Public" and "Private"
// networks, and when an app first listens it shows a dialog that, by
// default, only allows Private networks and silently creates a *block*
// rule for Public ones. Most home Wi-Fi networks end up classified as
// Public, so the phone just sees a blank page.
//
// Tapis therefore creates its own inbound rule, scoped as tightly as
// possible: this executable only, TCP on Tapis' port only, and only from
// addresses of the local subnet (Windows' "LocalSubnet" keyword), on every
// profile. It also removes the rules Windows' dialog created for this
// executable, since a block rule always wins over an allow rule.
// Creating a firewall rule needs administrator rights: Windows shows one
// UAC prompt, the first time only.

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func encode(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func powershell(args ...string) *exec.Cmd {
	cmd := exec.Command("powershell.exe", append([]string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass"}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func ruleName(port int) string { return fmt.Sprintf("Tapis (TCP %d, local network)", port) }

func check(exe string, port int) (bool, error) {
	script := fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$p = %s; $n = %s
$rs = @(Get-NetFirewallApplicationFilter -Program $p | Get-NetFirewallRule | Where-Object { $_.Direction -eq 'Inbound' -and $_.Enabled -eq 'True' })
$block = @($rs | Where-Object { $_.Action -eq 'Block' }).Count
$ours = @($rs | Where-Object { $_.DisplayName -eq $n -and $_.Action -eq 'Allow' }).Count
if ($block -eq 0 -and $ours -gt 0) { 'ok' } else { 'fix' }`, psQuote(exe), psQuote(ruleName(port)))
	out, err := powershell("-EncodedCommand", encode(script)).Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "ok", nil
}

func fix(exe string, port int) error {
	script := fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$p = %s; $n = %s
Get-NetFirewallApplicationFilter -Program $p | Get-NetFirewallRule | Where-Object { $_.Direction -eq 'Inbound' } | Remove-NetFirewallRule
Get-NetFirewallRule -DisplayName 'Tapis (TCP *' | Remove-NetFirewallRule
$ErrorActionPreference = 'Stop'
New-NetFirewallRule -DisplayName $n -Description 'Lets phones on the local network reach Tapis (phone trackpad and keyboard). Created by Tapis.' -Direction Inbound -Action Allow -Program $p -Protocol TCP -LocalPort %d -RemoteAddress LocalSubnet -Profile Any | Out-Null`,
		psQuote(exe), psQuote(ruleName(port)), port)
	// Start-Process -Verb RunAs shows the UAC prompt and waits for the
	// elevated PowerShell to finish.
	launcher := fmt.Sprintf(`Start-Process -FilePath powershell.exe -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-EncodedCommand','%s'`, encode(script))
	if out, err := powershell("-Command", launcher).CombinedOutput(); err != nil {
		if strings.Contains(string(out), "canceled") || strings.Contains(string(out), "annul") {
			return errors.New("the administrator permission request was declined")
		}
		return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Ensure checks the firewall and, if needed, creates Tapis' rule. announce
// is called right before Windows shows the administrator prompt.
func Ensure(port int, announce func()) (Status, error) {
	exe, err := os.Executable()
	if err != nil {
		return Failed, err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	ok, err := check(exe, port)
	if err != nil {
		return Failed, fmt.Errorf("cannot read the Windows Firewall configuration: %w", err)
	}
	if ok {
		return AlreadyOK, nil
	}
	announce()
	if err := fix(exe, port); err != nil {
		return Failed, err
	}
	if ok, _ := check(exe, port); !ok {
		return Failed, errors.New("the firewall rule was not created (was the administrator prompt declined?)")
	}
	return Configured, nil
}
