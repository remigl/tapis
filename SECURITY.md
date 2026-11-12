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
| Network reach | Only private addresses (192.168/16, 10/8, 172.16/12, link-local, loopback) are served. The Windows firewall rule is limited to Tapis' executable, its port and the local subnet. No Internet access is ever needed or made. |
| Encryption | TLS 1.3 only, with Go's modern defaults (AES-GCM / ChaCha20-Poly1305, X25519 and the post-quantum hybrid X25519MLKEM768). HTTP/2 disabled to keep the attack surface small. |
| Server identity | A local certificate authority generated on first start, **name-constrained** to private addresses and `.local` names: even if its key leaked, it could not impersonate a public website. Keys stay in the user's configuration folder (`0600`). |
| Pairing | Random 128-bit code, single use, valid 5 minutes, carried in the URL fragment (never sent over the network, removed from history). After 20 wrong codes in total, the code is cancelled. The QR image is deleted as soon as it is used or expired. |
| Two-sided consent | When Tapis runs in a window, every new phone must also be **approved on the computer** (type `y`), like Bluetooth pairing. A photographed QR code is not enough. |
| Authentication | Each phone gets its own 256-bit key; the computer stores only its SHA-256 hash and compares in constant time. A connection must authenticate within 10 s. 5 failures from one address block it for one minute. |
| Revocation | `tapis revoke <id>` or `r <id>` in the window. Sessions of a revoked phone are closed (immediately from the window, within 15 s from the command line). |
| Web isolation | WebSocket `Origin` check; `Host` allow-list (IP literals, `localhost`, the computer's `.local` name) which defeats DNS-rebinding attacks; strict Content-Security-Policy (no inline code, no external resource, Trusted Types enforced); `frame-ancestors 'none'`, `nosniff`, `no-referrer`, COOP/CORP, a restrictive Permissions-Policy. |
| Robustness | Size limits on every message (64 KiB) and value (pointer, scroll, text, repeat counts), RFC-compliant control frames only, header/read timeouts, at most 16 connections (4 per address), bounded bookkeeping. Pressed buttons and keys are released if the phone disconnects. |
| System actions | Lock and sleep run fixed commands/APIs; nothing received from the network is passed to a shell. Sleep requires holding the button on the phone. |
| Supply chain | Zero third-party Go dependencies (standard library only). CI actions pinned to commit SHAs, least-privilege tokens, `govulncheck`, CodeQL (Go and JavaScript), Dependabot, race-detector tests. |
| Release integrity | Reproducible `-trimpath` builds, `SHA256SUMS`, and a **Sigstore-signed build provenance attestation** for every file. Windows Authenticode signing is prepared (see [CODE_SIGNING.md](CODE_SIGNING.md)). |

### Verifying a download

```sh
gh attestation verify tapis-v0.2.0-windows-amd64.exe --repo remigl/tapis
```

This proves the file was built by this repository's workflow from a public commit, and was not modified afterwards.

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

1. **First connection trust.** A web page cannot pin a certificate, so the first time you accept Tapis' certificate in the browser. Someone able to intercept your Wi-Fi traffic at that exact moment could impersonate the computer. Mitigations: pair on a network you trust; the approval on the computer shows the phone's name and address; and for complete protection **install Tapis' certificate authority on the phone** (README › Security) — the browser then verifies the computer every time and no warning ever appears. If a certificate warning appears again on the same network, do not continue.
2. **Linux `/dev/uinput` access.** The udev rule lets programs of the logged-in user create virtual input devices (they can type, not read what you type). Programs you run can already control your session in other ways on most desktops.
3. **Public networks.** Tapis works on any network but stays reachable by devices on it; they still cannot pair or connect without your approval and a key. Close Tapis when you don't use it on untrusted networks.
4. **Unsigned Windows binaries** until Authenticode signing is enabled: verify downloads with the attestation above.
