# Security Policy

## Reporting a vulnerability

If you discover a security vulnerability in Portato, **please do not open a
public issue.** Report it privately via one of:

1. **GitHub private vulnerability reporting** (preferred) — the
   "Report a vulnerability" button on the
   [Security tab](https://github.com/portuber/portato/security/advisories/new).
2. **Email** — [kipkaev@kipkaev.name](mailto:kipkaev@kipkaev.name).

Please include a description, reproduction steps, and the impact. A
proof-of-concept is appreciated but not required.

## Scope

Portato handles SSH credentials and local privileged surfaces, so the following
are in scope:

- SSH authentication material: identity files, passphrases (OS keyring cache),
  and interactive passwords.
- The daemon IPC bearer token and the unix-socket / named-pipe permissions.
- Autostart entries written by `portato install` (launchd / systemd / the
  Windows SCM service, whose install-time account password SCM stores as an LSA
  secret).
- Supply chain: vulnerable dependencies (`govulncheck` runs weekly in CI).

Out of scope: vulnerabilities in upstream OpenSSH or the Go toolchain itself —
report those upstream.

## Supported versions

Only the latest release receives security fixes.

| Version | Supported |
|---------|-----------|
| latest  | ✅        |
| older   | ❌        |

## Disclosure

Coordinated disclosure: reports are acknowledged within ~72 hours, a fix is
prepared for the next release, and details are published (with credit, if
desired) once the fixed release is out.
