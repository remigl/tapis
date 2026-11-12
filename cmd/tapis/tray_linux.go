//go:build linux

package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/remigl/tapis/internal/server"
	"github.com/remigl/tapis/internal/sni"
)

//go:embed icons/*.png
var trayIcons embed.FS

var (
	trayMu sync.Mutex
	tray   *sni.Tray
)

func linuxFrench() bool {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if l := os.Getenv(v); l != "" {
			return strings.HasPrefix(l, "fr")
		}
	}
	return false
}

func ltr(fr, en string) string {
	if linuxFrench() {
		return fr
	}
	return en
}

func trayRefresh() {
	trayMu.Lock()
	t := tray
	trayMu.Unlock()
	if t != nil {
		t.Refresh()
	}
}

// openFile opens a file or URL with the desktop's default application.
func openFile(p string) {
	for _, c := range []string{"/usr/bin/xdg-open", "/usr/bin/gio"} {
		if _, err := os.Stat(c); err == nil {
			args := []string{p}
			if strings.HasSuffix(c, "gio") {
				args = []string{"open", p}
			}
			exec.Command(c, args...).Start()
			return
		}
	}
}

func autostartFile() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "autostart", "tapis.desktop")
}

func autostartOn() bool {
	_, err := os.Stat(autostartFile())
	return err == nil
}

func setAutostart(on bool) error {
	f := autostartFile()
	if !on {
		return os.Remove(f)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(f), 0o755)
	desktop := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=Tapis\nComment=%s\nExec=%q\nTerminal=false\nX-GNOME-Autostart-enabled=true\n",
		ltr("Téléphone comme trackpad et clavier", "Phone as trackpad and keyboard"), exe)
	return os.WriteFile(f, []byte(desktop), 0o644)
}

func startTray(srv *server.Server, logger *log.Logger, dir string, quit func()) {
	var pngs [][]byte
	for _, s := range []int{22, 24, 32, 48, 64} {
		if b, err := trayIcons.ReadFile(fmt.Sprintf("icons/tray-%d.png", s)); err == nil {
			pngs = append(pngs, b)
		}
	}
	showQR := func() {
		showPairing(srv, dir) // also printed in the terminal, if any
		openFile(filepath.Join(dir, "pairing-qr.png"))
	}
	t, err := sni.Start(sni.Options{
		Title:    "Tapis",
		IconPNGs: pngs,
		Activate: showQR,
		Tooltip: func() string {
			n := len(srv.Connected())
			if n == 0 {
				return ltr("Aucun téléphone connecté", "No phone connected")
			}
			return fmt.Sprintf(ltr("%d téléphone(s) connecté(s)", "%d phone(s) connected"), n)
		},
		Menu: func() []sni.Item {
			var items []sni.Item
			names := srv.Connected()
			switch len(names) {
			case 0:
				items = append(items, sni.Item{Label: ltr("Aucun téléphone connecté", "No phone connected"), Disabled: true})
			case 1:
				items = append(items, sni.Item{Label: ltr("1 téléphone connecté :", "1 phone connected:"), Disabled: true})
			default:
				items = append(items, sni.Item{Label: fmt.Sprintf(ltr("%d téléphones connectés :", "%d phones connected:"), len(names)), Disabled: true})
			}
			for _, n := range names {
				items = append(items, sni.Item{Label: "    " + n, Disabled: true})
			}
			return append(items,
				sni.Item{Sep: true},
				sni.Item{Label: ltr("Afficher le QR code (associer un téléphone)", "Show the QR code (pair a phone)"), OnClick: showQR},
				sni.Item{Label: ltr("Lancer à l'ouverture de session", "Start at login"), Toggle: true, Checked: autostartOn(),
					OnClick: func() {
						if err := setAutostart(!autostartOn()); err != nil {
							logger.Printf("autostart: %v", err)
						}
					}},
				sni.Item{Sep: true},
				sni.Item{Label: ltr("Aide et dépannage", "Help and troubleshooting"), OnClick: func() { openFile(ltr(helpURLFR, helpURL)) }},
				sni.Item{Label: ltr("Quitter Tapis", "Quit Tapis"), OnClick: quit},
			)
		},
	})
	if err != nil {
		logger.Printf("tray icon not shown: %v", err)
		return
	}
	trayMu.Lock()
	tray = t
	trayMu.Unlock()
}

// desktopConfirm asks for pairing approval with a dialog box when Tapis
// runs without a terminal. Without a dialog tool, pairing is refused.
func desktopConfirm(name, ip, code string) bool {
	text := fmt.Sprintf(ltr(
		"Le téléphone « %s » (%s) veut contrôler cet ordinateur.\n\nVérifie que ton téléphone affiche le code :  %s %s\n\nL'autoriser ?",
		"The phone \"%s\" (%s) wants to control this computer.\n\nCheck that your phone shows the code:  %s %s\n\nAllow it?"), name, ip, code[:3], code[3:])
	for _, c := range [][]string{
		{"/usr/bin/zenity", "--question", "--title=Tapis", "--no-markup", "--text=" + text},
		{"/usr/bin/kdialog", "--title", "Tapis", "--yesno", text},
	} {
		if _, err := os.Stat(c[0]); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 60e9)
			err := exec.CommandContext(ctx, c[0], c[1:]...).Run()
			cancel()
			return err == nil // exit status 0 = Yes
		}
	}
	return false
}
