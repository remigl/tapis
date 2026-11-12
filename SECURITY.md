# Security

Tapis lets a phone type and click on a computer, so it is built to keep that power in the right hands only. This document describes the threat model, the protections, the results of the security audit and the limits that remain.

## Reporting a vulnerability

Please do **not** open a public issue. Use GitHub's private reporting: *Security → Report a vulnerability* on this repository. You will get an answer within a week.

## Threat model

**Protected against:** other people and devices on the same network (home, shared flat, café, hotel), web pages open on the phone or on the computer, someone who sees or photographs the pairing QR code, a lost or stolen phone (after revocation), tampered downloads.

**Out of scope:** malware already running on the computer or the phone (it can control them anyway), someone with physical access to the unlocked computer, a phone whose owner installs a malicious certificate authority.

## Protections

| Layer | Measure |
|---|---|
| Network reach | Only devices **on the same subnet** as the address Tapis is offered on (and loopback) are served; peers reached through a VPN, a container/VM network or a router are refused before any TLS work, even with a private address. The Windows firewall rule is limited to Tapis' executable, its port and the local subnet. No Internet access is ever needed or made. |
| Encryption | TLS 1.3 only, with Go's modern defaults (AES-GCM / ChaCha20-Poly1305, X25519 and the post-quantum hybrid X25519MLKEM768). HTTP/2 disabled to keep the attack surface small. |
| Server identity | A local certificate authority generated on first start, **name-constrained** to private addresses and `.local` names: even if its key leaked, it could not impersonate a public website. Keys stay in the user's configuration folder (`0600`). |
| Pairing | Random 128-bit code, single use, valid 5 minutes, carried in the URL fragment (never sent over the network, removed from history). After 20 wrong codes in total, the code is cancelled. The QR image is deleted as soon as it is used or expired. |
| Two-sided consent | Every new phone must also be **approved on the computer** (dialog on Windows, `y` in the terminal), and both screens show the same random **6-digit code** (numeric comparison, like Bluetooth): a photographed QR code is not enough, and a request from another device is visible. |
| Authentication | Each phone gets its own 256-bit key; the computer stores only its SHA-256 hash and compares in constant time. A connection must authenticate within 10 s. 5 failures from one address block it for one minute. |
| Revocation | `tapis revoke <id>` or `r <id>` in the window. Sessions of a revoked phone are closed (immediately from the window, within 15 s from the command line). |
| Web isolation | WebSocket `Origin` check; `Host` allow-list (IP literals, `localhost`, the computer's `.local` name) which defeats DNS-rebinding attacks; strict Content-Security-Policy (no inline code, no external resource, Trusted Types enforced); `frame-ancestors 'none'`, `nosniff`, `no-referrer`, COOP/CORP, a restrictive Permissions-Policy. |
| Robustness | Size limits on every message (64 KiB) and value (pointer, scroll, text, repeat counts), RFC-compliant control frames only, header/read/idle timeouts, at most 256 TCP connections (32 per address) and 16 WebSockets (4 per address), bounded bookkeeping. Pressed buttons and keys are released if the phone disconnects. "Forget this computer" on the phone also deletes its key on the computer. |
| System actions | Lock and sleep run fixed commands/APIs; nothing received from the network is passed to a shell. Sleep requires holding the button on the phone. |
| Text-field detection | On Windows, only one bit is read and sent — "the focused element accepts text" — never its content, name or application; only to paired phones that enabled the option. The phone opens its keyboard only right after a touch. |
| Linux tray | Exported on the user's own session bus (not reachable by other users or the network). Its actions only show the QR code, toggle autostart or quit; pairing still needs approval with the verification code. Without a terminal or a dialog tool, pairing is refused. |
| Now playing | The title of the current media is read locally (Windows media controls, Linux MPRIS), only while a phone is connected, and sent only to paired phones. Control characters are stripped and lengths bounded; the phone displays it as plain text. |
| Windows process | DLLs are loaded from System32 only (no DLL planting from the Downloads folder); PowerShell is always run by absolute path (no PATH hijacking); the single instance, log and keys stay in the user's profile. |
| Supply chain | Zero third-party Go dependencies (standard library only). CI actions pinned to commit SHAs, least-privilege tokens, `govulncheck`, CodeQL (Go and JavaScript), Dependabot, race-detector tests, fuzzing of every parser, and an attack test suite run on every change. |
| Release integrity | Reproducible `-trimpath` builds, `SHA256SUMS`, and a **Sigstore-signed build provenance attestation** for every file. Windows Authenticode signing is prepared (see [CODE_SIGNING.md](CODE_SIGNING.md)). |

### Verifying a download

```sh
gh attestation verify tapis-v0.2.0-windows-amd64.exe --repo remigl/tapis
```

This proves the file was built by this repository's workflow from a public commit, and was not modified afterwards.

## Lock-screen helper (Windows)

Optional, off by default. Windows isolates the lock screen on a secure desktop; only a SYSTEM process on that desktop can type there.

- Enabling it (menu → *Type on the lock screen*) copies Tapis to `C:\Program Files\Tapis\` — writable by administrators only, so a user-level program cannot swap the file — and registers the scheduled task *Tapis lock screen helper*, running as SYSTEM. The elevated installer verifies the SHA-256 of the file before and after copying, so it cannot be replaced while the administrator prompt is open.
- That task starts an agent in the logged-on session, on the secure desktop. The agent **injects input only while the session is locked** (it checks the session state before every event), so it cannot be used to approve a UAC prompt or a Ctrl+Alt+Del screen of an unlocked session.
- The agent listens on a local named pipe whose access list allows only SYSTEM and the user logged on in that session; remote clients are rejected, and low-integrity (sandboxed) processes cannot write to it.
- Before sending anything, Tapis checks that the pipe belongs to a SYSTEM process, so another program cannot pose as the agent to capture what you type; it connects anonymously, so the pipe server cannot impersonate it.
- The agent never opens a network connection; all values are bounded as on the network side.
- Residual risk: a program already running under your account could, while your session is locked, send keystrokes to the lock screen through the pipe. It would still need your PIN (and Windows limits PIN attempts). Disable the helper if you do not need it.
- Removing it (same menu entry) deletes the task and the files.

## Security audit — version 0.5.0 (November 2026)

**Scope:** the whole repository — network server (HTTP/TLS/WebSocket), pairing and authentication, certificate authority, device store, input injection (Windows, Linux), Windows notification-area UI, firewall configuration, lock-screen helper (SYSTEM), media reader, system actions, phone web app, build and release pipeline.

**Method:** threat modelling (STRIDE) per trust boundary; line-by-line review with an attacker's view; dynamic testing against a running server (`internal/server/attack_test.go`: cross-origin and DNS-rebinding requests, unauthenticated commands, oversized headers and frames, pairing-code and key guessing from many source addresses, connection floods, TLS downgrade); coverage-guided fuzzing of every parser (WebSocket frames, protocol messages, QR encoder, media metadata, keyboard layouts); static analysis (`go vet`, CodeQL `security-extended`, `govulncheck`). All tests run in CI on every change.

**Trust boundaries:** (1) local network ↔ server; (2) web page ↔ other web origins on the phone; (3) user ↔ SYSTEM (lock-screen helper); (4) user process ↔ other local programs (PATH, DLLs, pipes, files); (5) source code ↔ released binaries.

| ID | Severity | Boundary | Finding | Fix |
|---|---|---|---|---|
| TAP-01 | **High** | 1 | Any private address could reach the server: a peer on a corporate/commercial VPN (10.x), a Docker/VM network or another subnet behind a router could attempt pairing and authentication. | Clients must be on the same subnet as the address Tapis is offered on; rejected at TCP accept, before TLS. The `Host` must be one of the computer's own addresses. |
| TAP-02 | **High** | 1 | Pairing approval only showed a name chosen by the phone: an attacker who obtained a code could name their device like yours and be approved by mistake. | Numeric comparison: a random 6-digit code is shown on the phone and in the approval dialog. |
| TAP-03 | **High** | 4 | Windows DLL planting: Tapis, typically run from the Downloads folder, loaded non-KnownDLLs (`wtsapi32.dll`, `powrprof.dll`) using the default search order, which includes the executable's folder. | `SetDefaultDllDirectories(SYSTEM32)` before any DLL is loaded. |
| TAP-04 | **Medium** | 4 | PowerShell (firewall, lock-screen installer, media) was started by name; a `powershell.exe` earlier in `PATH` would have run instead — including at the administrator prompt. | Absolute path from `GetSystemDirectory`. Linux system commands also use absolute paths. |
| TAP-05 | **Medium** | 3 | Time-of-check/time-of-use while installing the SYSTEM helper: the executable could be replaced between the user's click and the copy. | SHA-256 checked by the elevated script before and after copying. |
| TAP-06 | **Medium** | 1 | Unbounded idle TCP connections (one goroutine each, plain-HTTP keep-alive without idle timeout) allowed a slowloris-style denial of service. | 256 connections in total, 32 per address; read, write and idle timeouts on both listeners. |
| TAP-07 | Low | 3 | The lock-screen pipe client allowed impersonation by the pipe server. | Anonymous security quality of service. |
| TAP-08 | Low | 1 | "Forget this computer" on the phone left the key valid on the computer. | The phone asks the computer to revoke its key. |
| TAP-09 | Low | 4 | A configuration folder created earlier with looser permissions was reused as is (Linux). | Permissions reset to `0700`. |
| TAP-10 | Info | 1 | Device identifiers were 32-bit (collision risk only). | 64-bit. |

No remote code execution, authentication bypass or injection was found. Fuzzing (WebSocket parser, protocol, QR, metadata, layouts) found no crash.

### Recommendations for the maintainer (GitHub settings)

- Enable **private vulnerability reporting** (*Settings → Code security*).
- Protect `main` and tags `v*` with a **ruleset** (no force-push or deletion, required status checks *test* and *CodeQL*).
- Use two-factor authentication with a security key or passkey on the GitHub account.
- Enable Windows code signing (see [CODE_SIGNING.md](CODE_SIGNING.md)).

## Audit (version 0.2.0)

A review of the whole code base (server, WebSocket and QR implementations, certificate handling, input injection, phone app, build pipeline) was performed. Findings and fixes:

| # | Severity | Finding | Status |
|---|---|---|---|
| 1 | High | DNS rebinding: a malicious domain re-pointed to the computer's address could have passed the Origin check, since it compared against the `Host` header. | Fixed: `Host` allow-list. |
| 2 | Medium | The pairing QR image (containing a live code) stayed on disk. | Fixed: deleted on use, expiry and exit. |
| 3 | Medium | A pairing code could be attacked from many addresses, each with its own failure budget. | Fixed: global limit, the code is cancelled after 20 wrong attempts. |
| 4 | Medium | A photographed QR code was enough to pair a phone. | Fixed: approval on the computer. |
| 5 | Low | `go 1.22` in `go.mod` kept older TLS defaults (no post-quantum key exchange). | Fixed: `go 1.24`. |
| 6 | Low | One address could open all connection slots; per-address tables were unbounded. | Fixed: per-address cap, bounded tables. |
| 7 | Low | WebSocket control frames larger than 125 bytes or fragmented were accepted. | Fixed: rejected per RFC 6455 §5.5. |
| 8 | Low | The plain-HTTP redirect could reflect an arbitrary `Host`. | Fixed by #1. |
| 9 | Info | No Trusted Types enforcement. | Added (`require-trusted-types-for 'script'`). |
| 10 | Info | CI actions referenced by mutable tags, no vulnerability scanning, unsigned releases. | Fixed: SHA pinning, govulncheck, CodeQL, attestations. |

Automated checks in CI: `go vet`, race-detector tests, `govulncheck`, CodeQL `security-extended`.

## Remaining limits (and how to remove them)

1. **First connection trust.** A web page cannot pin a certificate, so the first time you accept Tapis' certificate in the browser. Someone able to intercept your Wi-Fi traffic at that exact moment could impersonate the computer. Mitigations: pair on a network you trust; the approval on the computer shows the phone's name, address and a verification code; and for complete protection **install Tapis' certificate authority on the phone** (README › Security) — the browser then verifies the computer every time and no warning ever appears. If a certificate warning appears again on the same network, do not continue.
2. **Linux `/dev/uinput` access.** The udev rule lets programs of the logged-in user create virtual input devices (they can type, not read what you type). Programs you run can already control your session in other ways on most desktops.
3. **Public networks.** Tapis works on any network but stays reachable by devices on it; they still cannot pair or connect without your approval and a key. Close Tapis when you don't use it on untrusted networks.
4. **Unsigned Windows binaries** until Authenticode signing is enabled: verify downloads with the attestation above.
