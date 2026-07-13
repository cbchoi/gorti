# Security Policy

## Supported versions

Security fixes are provided for the latest tagged release and the `main`
branch. Older releases may be asked to upgrade before receiving a fix.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Email
`me@cbchoi.info` with:

- the affected version or commit;
- deployment and configuration details;
- reproduction steps or a proof of concept;
- the expected impact; and
- any suggested mitigation.

You should receive an acknowledgement within seven days. The project will
coordinate validation, remediation, disclosure timing, and credit with the
reporter.

## Deployment boundary

The plaintext federate and admin listeners assume a trusted network. Use TLS
and the documented authentication options for untrusted networks, and keep
the admin listener on loopback unless it is protected by an external access
control layer.
