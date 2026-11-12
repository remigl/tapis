# Troubleshooting

[Français](DEPANNAGE.md)

Start with the checklist, it solves most problems in a minute. Then look for your symptom below.

## Quick checklist

1. Tapis is running on the computer and its window shows `Firewall: OK` (Windows).
2. The phone is on the **same Wi-Fi network** as the computer (not on mobile data, not on a "guest" network) and its **VPN is off** or allows local network access.
3. You open the address shown by Tapis (`https://192.168.x.x:47800`) **in Chrome** (Android) or **Safari** (iPhone).
4. On the certificate warning, you tapped **Advanced → Proceed**.

To test the computer side alone, open `https://localhost:47800` in a browser on the computer. If the dark Tapis page appears, Tapis works and the problem is between the phone and the computer.

---

## The page stays white / keeps loading, or "This site can't be reached"

The phone cannot reach the computer. Causes, from most to least likely:

**Windows Firewall blocks Tapis.** Since version 0.1.1 Tapis configures the firewall itself: at the first start Windows asks for administrator permission, answer **Yes**. If you answered No, or the window shows `Firewall: NOT configured`, close Tapis and start it again to get the prompt back.
The rule Tapis creates is narrow: this program only, its port only, and only from your local network, so it is safe on any network.
Manual fix (PowerShell **as administrator**; adapt the path):
```powershell
New-NetFirewallRule -DisplayName "Tapis (TCP 47800, local network)" -Direction Inbound -Action Allow `
  -Program "C:\path\to\tapis.exe" -Protocol TCP -LocalPort 47800 -RemoteAddress LocalSubnet -Profile Any
```
Also remove any rule named after the Tapis program that **blocks** it in *Windows Defender Firewall → Advanced settings → Inbound rules*: a block rule always wins.

**Another security program filters the network** (Norton, McAfee, Kaspersky, Bitdefender, ESET…). They replace Windows Firewall: allow `tapis.exe` for the local network in their settings.

**The phone is not on the same network.** Check that Wi-Fi is on and connected to the same network as the computer. Guest networks, hotel, campus and some café Wi-Fi isolate devices from each other ("client isolation"): Tapis cannot work there. Use your phone's hotspot instead: connect the computer to the phone's hotspot, then start Tapis again (a new address is shown).

**The phone and the computer are on different subnets.** For security, Tapis only answers devices on the same subnet as the computer (the first three numbers of the address are usually identical, e.g. `192.168.1.x`). Some mesh systems or routers put Wi-Fi and Ethernet on different subnets: connect both devices to the same network. The log (menu → *Open the log*) shows `rejected connection from …` in that case.

**Tapis shows the wrong address.** Compare with the IPv4 address of your Wi-Fi/Ethernet adapter given by `ipconfig` (Windows) or `ip addr` (Linux). If the computer has several networks, Tapis lists them all under "other network": try each one.

**A VPN is active** on the computer or the phone. Some VPNs block local network access: disable it, or enable "allow LAN access" in the VPN settings.

**Linux firewall.** If you use ufw: `sudo ufw allow from 192.168.0.0/16 to any port 47800 proto tcp` (adapt to your network). firewalld: `sudo firewall-cmd --zone=home --add-port=47800/tcp --permanent && sudo firewall-cmd --reload` and make sure your Wi-Fi is in the `home` zone.

## It works on Wi-Fi but not when the phone's VPN is on

A VPN sends all the phone's traffic into an encrypted tunnel to a remote server, including traffic meant for your own network, so the phone cannot reach the computer. No website can bypass the phone's VPN, but every good VPN app has an option for it:

- **Proton VPN:** Settings → *LAN connections* (Allow LAN connections).
- **NordVPN:** *Split tunneling* → exclude Chrome.
- **Surfshark, Mullvad, ExpressVPN, WireGuard and others:** look for *Local network sharing*, *Allow LAN*, *Bypass LAN* or *Split tunneling* (exclude Chrome).
- **Android Private DNS** does not cause this; only VPNs do.

After changing the setting, reload the Tapis page.

## "Confirm on the computer" is shown and nothing happens

Each new phone must be approved on the computer: answer **Yes** in the dialog (Windows) or type `y` then Enter in the terminal (Linux). Both screens show the same 6-digit code: if they differ, refuse — the request does not come from your phone. Without an answer within one minute the pairing is refused. If Tapis runs without a window (Linux service), start it with `tapis --pair` in a terminal to pair.

## Lock or sleep does nothing (Linux)

Lock uses `loginctl lock-session` (or `xdg-screensaver lock`) and sleep uses `systemctl suspend`. Check that they work when typed in a terminal; some desktops need a screen locker to be installed.

## The page stays white right after scanning, but works when typed in Chrome

The camera app opened the link in its built-in viewer, which cannot show the certificate warning. In the camera or Google Lens, choose **Open in Chrome** (⋮ menu), or copy the address shown by Tapis into Chrome.

## "Your connection is not private" / NET::ERR_CERT_AUTHORITY_INVALID

Expected the first time: the certificate was created by your computer, not by a public authority. Tap **Advanced → Proceed to …**. Your browser remembers the choice.
You will see it again when the computer's address changes (another network) — that's normal. If it appears again **on the same network** without you resetting Tapis, do not continue: someone may be intercepting the connection.
To get rid of the warning for good, install Tapis' certificate on the phone: see [README › Security](README.md#security).

## "Pairing failed": the code was already used or has expired

A QR code works once and for 5 minutes. In the Tapis window type `p` then Enter, and scan the new code.

## "Not paired" when opening the address

This browser has never been paired (or its data was cleared, or you use another browser / private tab). Show a new code with `p` + Enter and scan it. Each browser is paired separately.

## "Phone not paired anymore"

The phone was unpaired with `tapis revoke` or `r <id>`, or the configuration folder was deleted. Pair it again with a new code.

## "Computer unreachable" after it worked

Tapis was closed, the computer went to sleep, or one of the devices changed network. Restart Tapis, wake the computer, check the Wi-Fi, then tap **Retry**.

## The media title is not shown

Tapis shows what the system itself knows is playing (the same as the Windows volume overlay or the Linux media widget). If nothing appears:

- **Browsers** (YouTube, Netflix, etc. in Chrome, Edge, Firefox) work. In Firefox, `media.hardwaremediakeys.enabled` must stay on (default).
- **VLC 3 on Windows** does not publish what it plays to Windows. VLC 4, the Films & TV / Media Player apps, Spotify and browsers do. On Linux VLC works.
- **Slideshows** (PowerPoint, photo viewers) are not media players for the system, so nothing is shown. That is expected.
- On Linux, the title is read through `busctl` (part of systemd).

## Tapis icon not visible (Windows)

Tapis runs in the notification area: click the `^` arrow next to the clock to see the hidden icons. Drag the Tapis icon onto the taskbar to keep it visible. Starting `tapis.exe` again shows the QR code window.

## I can move the pointer on the lock screen but not type (Windows)

That is Windows protecting the lock screen. Right-click the Tapis icon → **Type on the lock screen** and accept the administrator prompt. If it was already enabled and stopped working after an update, untick and tick it again so the helper is updated.

## The phone keyboard does not open by itself

- The option is in the app settings: *Open the keyboard when a text field is selected on the computer*.
- It works on **Windows**, using the accessibility information of applications (like the Windows touch keyboard does). Most applications and all major browsers provide it; a few (some games, very old or custom-drawn apps, apps running as administrator while Tapis is not) do not: open the keyboard with the button.
- The keyboard opens right after you click from the phone; browsers forbid a page to open it on its own at other times.
- On Linux, this is not available yet: use the keyboard button.

## No tray icon on Linux

GNOME needs the *AppIndicator and KStatusNotifierItem Support* extension. Other desktops show it directly. Without a tray, Tapis keeps working in the terminal.

## The pointer moves but some windows don't react (Windows)

Windows prevents normal programs from controlling programs started **as administrator** (Task Manager, installers, some games), as well as Ctrl+Alt+Del and UAC prompts. Start Tapis as administrator if you need to control those windows. For the lock screen, see above.

## Wrong characters are typed (Linux)

Tapis must know your keyboard layout. It reads it from the system; if it guesses wrong, start it with `--layout be` (Belgian AZERTY), `--layout fr` (French AZERTY) or `--layout us`.
Characters that are not on your layout are typed with Ctrl+Shift+U; this works in most GTK/Qt applications but not everywhere.

## "cannot open /dev/uinput: permission denied" (Linux)

Install the udev rule once, then log out and back in:
```sh
sudo cp packaging/linux/60-tapis-uinput.rules /etc/udev/rules.d/
echo uinput | sudo tee /etc/modules-load.d/uinput.conf
sudo modprobe uinput && sudo udevadm control --reload && sudo udevadm trigger
```

## "port 47800 is already in use"

Tapis is already running (maybe in another window or in the background), or another program uses that port. Close the other Tapis, or start with `--port 47801` and pair again.

## Windows says "Windows protected your PC" (SmartScreen) or the antivirus warns

Tapis is not signed with a paid certificate, so Windows does not know it. Click **More info → Run anyway**. You can check the file against `SHA256SUMS` in the release, or build it yourself from the source code.

## The window closes immediately (Windows)

Since version 0.1.1 the window stays open and shows the error. With an older version, open a terminal (`cmd`), go to the folder and start `tapis.exe` from there to read the message.

## The phone screen turns off while I use it

Keeping the screen on needs a trusted certificate. Either install Tapis' certificate (see README › Security) or increase the screen timeout in the phone settings.

---

Still stuck? [Open an issue](https://github.com/remigl/tapis/issues) with: your system (Windows/Linux version), what the Tapis window shows, and what you see on the phone. Never post your pairing link or the content of the configuration folder.
