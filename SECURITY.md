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

Use TLS and the documented authentication options when traffic crosses an
untrusted network. Connected federate clients must still be trusted: transport
authentication does not provide complete per-federate authorization. Keep
the plaintext admin and metrics listeners on loopback unless they are protected
by an external access-control layer. See [operations](docs/operations.md).
