//go:build windows

package media

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unicode/utf16"
)

// A small PowerShell loop reads the Global System Media Transport Controls
// (a WinRT API) and prints one JSON line whenever the current media
// changes. It exits when Tapis exits.
const script = `$ErrorActionPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' })[0]
function Await($op, [Type]$type) { $t = $asTask.MakeGenericMethod($type).Invoke($null, @($op)); if ($t.Wait(5000)) { $t.Result } }
$null = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType = WindowsRuntime]
$mgr = Await ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager]::RequestAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager])
$last = $null
while ($true) {
  if (-not (Get-Process -Id %d -ErrorAction SilentlyContinue)) { exit }
  $o = [ordered]@{ title = ''; artist = ''; app = ''; playing = $false }
  $s = $null
  if ($mgr) { $s = $mgr.GetCurrentSession() }
  if ($s) {
    $p = Await ($s.TryGetMediaPropertiesAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties])
    if ($p) { $o.title = [string]$p.Title; $o.artist = [string]$p.Artist }
    $o.app = [string]$s.SourceAppUserModelId
    $o.playing = ([string]$s.GetPlaybackInfo().PlaybackStatus -eq 'Playing')
  }
  $j = ConvertTo-Json -InputObject $o -Compress
  if ($j -ne $last) {
    try { [Console]::Out.WriteLine($j); [Console]::Out.Flush() } catch { exit }
    $last = $j
  }
  Start-Sleep -Milliseconds 800
}`

func encode(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func watch(ctx context.Context, fn func(Info)) {
	enc := encode(fmt.Sprintf(script, os.Getpid()))
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", enc)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			return
		}
		var last Info
		first := true
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			var i Info
			if json.Unmarshal(sc.Bytes(), &i) != nil {
				continue
			}
			i = Normalize(i)
			if first || i != last {
				fn(i)
				last, first = i, false
			}
		}
		cmd.Wait()
		// PowerShell stopped unexpectedly: retry a bit later.
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
	}
}
