# Security policy

SnmpLens holds credentials for network equipment and runs on the workstation
that can reach it. Reports are welcome and are treated as the priority they
deserve.

The full policy — what the application does with credentials, what it never
writes down, how releases are signed, and what is deliberately *not* treated as
a vulnerability — is published at **<https://snmplens.com/security.html>**.
This file is the short form GitHub reads; that page is the source of truth, and
anything here that disagrees with it is a mistake in here.

## Reporting a vulnerability

**Please do not open a public issue.** Use GitHub's private vulnerability
reporting on this repository: **Security → Report a vulnerability**. It opens a
thread visible only to the maintainers.

What makes a report actionable:

- the version and the platform;
- what an attacker gains, and what access they need to start;
- steps to reproduce — a failing test is the clearest possible form.

This project is maintained in someone's own time, so please do not expect a
same-day answer. Reports are acknowledged, and a fix ships in a release whose
notes describe the problem.

## Supported versions

Fixes go into the next release from `main`. There are no long-term support
branches: the application updates itself, and the answer to "am I affected" is
normally "update".

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Anything older | No — update |

## Verifying what you downloaded

Every release carries a SHA-256 manifest signed with Ed25519. The in-app
updater verifies that signature against a key compiled into the binary *before*
it trusts the manifest, and refuses an update whose signature is missing rather
than applying it. The manifest's first line names its own tag, so an older
signed manifest cannot be replayed in place of a current one.

By hand:

```sh
gh release download v1.5.0 -p 'SnmpLens-checksums.txt*'
sha256sum -c SnmpLens-checksums.txt --ignore-missing
```

And independently of anyone holding the release key, provenance is attested by
the workflow that built the artifacts:

```sh
gh attestation verify SnmpLens-windows-amd64-setup.exe --repo SnmpLens/SnmpLens
```

## Out of scope

These are known, deliberate, and documented on the security page. A report
about one of them will be closed with a pointer to it, not because it is
unwelcome but because it is already answered.

- **SNMP v1 and v2c send the community string in the clear.** That is the
  protocol. SnmpLens supports them because networks still use them; use v3
  where you can.
- **Anonymous Mode is a display mask, not encryption.** The underlying data is
  unchanged and an export contains real values.
- **The credential store defends against a copied profile and against other
  accounts on the machine — not against code already running as you.** While
  the application runs, credentials are in its memory in the clear, because
  every request builder needs them.
- **A SET writes to live equipment, with no confirmation step.** That is by
  design; a dialog is not a control, and it is defeated by the same script that
  would have issued the SET directly.
- **Findings from a scanner with no demonstrated impact.** Say what an attacker
  gains and how they start.
