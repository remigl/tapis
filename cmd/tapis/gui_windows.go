//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/remigl/tapis/internal/firewall"
	"github.com/remigl/tapis/internal/lockscreen"
	"github.com/remigl/tapis/internal/qr"
	"github.com/remigl/tapis/internal/server"
	"github.com/remigl/tapis/internal/winui"
)

var french = winui.IsFrench()

func tr(fr, en string) string {
	if french {
		return fr
	}
	return en
}

func guiAvailable() bool { return true }

// alreadyRunning asks a running Tapis to show its QR code, and reports
// whether one was running.
func alreadyRunning() bool { return winui.SignalRunningInstance() }

func openLog(dir string) io.Writer {
	os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "tapis.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 1<<20 {
		os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return io.Discard
	}
	return f
}

func guiError(err error) {
	winui.MessageBox("Tapis", fmt.Sprintf("%s\n\n%v\n\n%s\n%s", tr("Tapis ne peut pas démarrer :", "Tapis cannot start:"),
		err, tr("Aide :", "Help:"), helpURL), false, true)
}

func guiConfirm(name, ip string) bool {
	return winui.MessageBox("Tapis", fmt.Sprintf(tr(
		"Le téléphone « %s » (%s) veut contrôler cet ordinateur.\n\nL'autoriser ?",
		"The phone \"%s\" (%s) wants to control this computer.\n\nAllow it?"), name, ip), true, false)
}

func guiPairingUsed() { winui.CloseQR() }

func lockscreenEnabled() bool { return lockscreen.Installed() }

func setLockscreen(on bool) error {
	if !on {
		if err := lockscreen.Uninstall(); err != nil {
			return err
		}
		winui.MessageBox("Tapis", tr("L'assistant de l'écran de verrouillage a été retiré.", "The lock-screen helper was removed."), false, false)
		return nil
	}
	if !winui.MessageBox("Tapis", tr(
		"Pour taper ton code PIN ou ton mot de passe sur l'écran de verrouillage depuis le téléphone, Tapis doit installer un petit assistant système.\n\n"+
			"• Il n'agit que lorsque la session est verrouillée (jamais sur les demandes d'administrateur).\n"+
			"• Seul ton compte peut lui parler, et il n'accède pas au réseau.\n"+
			"• Il est installé dans Program Files et se retire depuis ce même menu.\n\n"+
			"Windows va demander l'autorisation administrateur. Continuer ?",
		"To type your PIN or password on the lock screen from the phone, Tapis needs to install a small system helper.\n\n"+
			"• It only acts while the session is locked (never on administrator prompts).\n"+
			"• Only your account can talk to it, and it has no network access.\n"+
			"• It is installed in Program Files and can be removed from this same menu.\n\n"+
			"Windows will ask for administrator permission. Continue?"), true, false) {
		return nil
	}
	if err := lockscreen.Install(); err != nil {
		return err
	}
	winui.MessageBox("Tapis", tr("C'est prêt : tu peux maintenant taper ton code sur l'écran de verrouillage depuis le téléphone.",
		"Done: you can now type your code on the lock screen from the phone."), false, false)
	return nil
}

func runGUI(srv *server.Server, logger *log.Logger, port int, dir string, pair, noFW bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	winui.RefreshAutostart()

	if !noFW {
		st, err := firewall.Ensure(port, func() {
			winui.MessageBox("Tapis", tr(
				"Pour que ton téléphone puisse joindre Tapis, le pare-feu Windows doit l'autoriser sur ton réseau local.\n\nWindows va demander l'autorisation administrateur (une seule fois). La règle n'autorise que Tapis, sur son port, depuis ton réseau local.",
				"So that your phone can reach Tapis, Windows Firewall must allow it on your local network.\n\nWindows will ask for administrator permission (once). The rule only allows Tapis, on its port, from your local network."),
				false, false)
		})
		logger.Printf("firewall: %v %v", st, err)
		if st == firewall.Failed {
			winui.MessageBox("Tapis", tr("Le pare-feu n'a pas pu être configuré : ton téléphone risque de ne pas pouvoir se connecter.\n\n",
				"The firewall could not be configured: your phone may not be able to connect.\n\n")+err.Error(), false, true)
		}
	}

	newPairing := func() winui.Pairing {
		urls := srv.NewPairing()
		code, _ := qr.Encode(urls[0])
		logger.Printf("new pairing code shown")
		return winui.Pairing{URL: urls[0], Other: urls[1:], Code: code}
	}
	devs, _ := srv.Store.List()

	go func() {
		if err := srv.Run(ctx); err != nil {
			logger.Printf("server error: %v", err)
			guiError(err)
			winui.Quit()
		}
	}()
	logger.Printf("Tapis %s started, addresses %v", version, srv.URLs())

	err := winui.Run(winui.Options{
		French:     french,
		NewPairing: newPairing,
		Tooltip: func() string {
			n := len(srv.Sessions())
			if n == 0 {
				return tr("Tapis – aucun téléphone connecté", "Tapis – no phone connected")
			}
			return fmt.Sprintf(tr("Tapis – %d téléphone(s) connecté(s)", "Tapis – %d phone(s) connected"), n)
		},
		AutostartEnabled: func() bool { return winui.AutostartPath() != "" },
		SetAutostart:     winui.SetAutostart,
		LockscreenOn:     lockscreenEnabled,
		SetLockscreen:    setLockscreen,
		OpenLog:          func() { winui.OpenPath(filepath.Join(dir, "tapis.log")) },
		OpenHelp:         func() { winui.OpenPath(tr(helpURLFR, helpURL)) },
		Quit: func() {
			cancel()
			time.Sleep(200 * time.Millisecond)
			winui.Quit()
		},
		Ready: func() {
			if len(devs) == 0 || pair {
				winui.ShowQR(newPairing())
			} else {
				winui.Notify("Tapis", tr("Tapis est prêt. Clique sur son icône pour afficher le QR code.",
					"Tapis is ready. Click its icon to show the QR code."))
			}
		},
	})
	if err != nil {
		guiError(err)
	}
}
