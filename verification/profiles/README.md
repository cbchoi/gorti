# Receive-order transport profiles

These JSON files select the completion policy used by the Go verification
federate and the Portico comparison harness.

- `local-lrc.json` uses bounded local admission, pipelined frames, cumulative
  ACKs, and a final flush.
- `confirmed.json` waits for the server result on every receive-order Object
  Management call.

Both files conform to `receive-order-transport.schema.json`. Unknown fields,
invalid values, and unsupported schema versions are rejected. Explicit
command-line options override values loaded from a profile. `--config` and
`-Config` are short aliases for `--transport-config` and `-TransportConfig`.
