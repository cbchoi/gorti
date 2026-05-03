# M6 W1B – Agent A hardening report

Branch: `agent/a/m6-w1b-concurrency-tls`
Scope: two post-MVP follow-ups bundled in one wave.

| Fix | Area | Status |
| --- | --- | --- |
| 1 | EventLog Writer concurrency (W2A finding) | done |
| 2 | gRPC server-side TLS for rtid | done (cut-1: static cert) |

---

## Fix 1 — EventLog Writer concurrency

### The latent bug

`rti/internal/eventlog/writer.go::Writer.Append` mutated `nextSeq` and
appended a length-prefixed record to `opts.Sink` with no internal
synchronization. The federation manager has historically been the
sole serializing caller (one Append goroutine per federation), so the
data race was not observable in the M0–M5 spec tests. Two latent risks
remained:

1. The gRPC handler set may legitimately drive `Append` from multiple
   goroutines on the same federation (object-update fan-in).
2. The W2A perf workload trips the race directly with a tight-loop
   sender; W2A's workaround was to run with `EventLog: nil`
   (`rti/internal/perf/baseline.go` lines 297–302 document this).

### Reproduction (before fix)

A new race test (`rti/internal/eventlog/writer_race_test.go`) drives 8
goroutines × 256 appends each against a single Writer:

```
$ go test -race -run TestWriter_Append_ConcurrentSafe \
      ./rti/internal/eventlog/...
WARNING: DATA RACE
… many "seq N assigned 4 times (want 1)" failures …
testing.go:1398: race detected during execution of test
FAIL    github.com/cbchoi/gorti/rti/internal/eventlog
```

Distinct seq count drops from the expected 2048 to ~1300 — concurrent
unsynchronized increments overwrite each other.

### The fix

Added a `sync.Mutex` to `Writer` covering `nextSeq` increments, the
`assignSeq` write-back into the caller's record, and the length-prefix
+ body write pair to the sink. `Append`, `Sync`, and `Close` all take
the mutex; the `Append` fast-fail checks (federation mismatch, nil
event) happen before grabbing the lock so they don't contend with
in-flight writers.

`MultiplexWriter` previously documented the inner Writer as
"goroutine-unsafe (per W1B's contract)". Comment updated to reflect
that the inner Writer now has its own mutex; the multiplex mutex
guards only the per-federation writer table (lazy create + close
walk).

### Verification (after fix)

```
$ go test -race ./rti/internal/eventlog/...
ok  github.com/cbchoi/gorti/rti/internal/eventlog  1.099s
```

All 36 existing eventlog tests still pass; the new race test passes
under `-race`.

### Implication for `rti/internal/perf/baseline.go`

The perf harness's `EventLog: nil` workaround can now be removed; the
multiplex writer is safe under tight-loop concurrent senders. **No perf
code is changed in this PR** (per task scope), but the contract is now
explicit and a future cut can wire a real `MultiplexWriter` into the
harness without race-detector failures.

---

## Fix 2 — gRPC server-side TLS for rtid

### Scope

W1C cut-1 used `grpc.aio.insecure_channel` on the Python SDK side. To
support TLS end-to-end, rtid needs a server-side TLS listener. This
fix adds:

- Two new flags on `rtid`: `--tls-cert` and `--tls-key`.
- A `buildServerTLS` helper that loads the keypair and returns a
  `*tls.Config` (with `MinVersion: TLS 1.2`).
- Threading via a new `rtidConfig.TLSConfig` field into `newRTID`,
  which constructs the gRPC server with `grpc.Creds(credentials.NewTLS(cfg))`.
- A documented connect URL convention.

mTLS, client cert validation, and cert rotation are deferred to M7.

### Connect URL convention

| URL form | rtid mode | Python SDK transport |
| --- | --- | --- |
| `grpc://host:port` | insecure (no TLS flags) | `grpc.aio.insecure_channel` (current SDK behavior) |
| `grpcs://host:port` | TLS (`--tls-cert` + `--tls-key`) | `grpc.aio.secure_channel` (Agent C follow-up) |

The Python SDK's `pysdk/rti1516e/_transport.py` already accepts the
`grpc://` form. Wiring the `grpcs://` branch is Agent C territory in
the next wave; this PR's responsibility ends at the Go server.

### Tests

`rti/cmd/rtid/tls_test.go` covers:

| Test | Asserts |
| --- | --- |
| `TestBuildServerTLS_Insecure` | empty paths → `(nil, nil)` |
| `TestBuildServerTLS_RejectsHalfPair` | `--tls-cert` without `--tls-key` (and vice-versa) → error |
| `TestBuildServerTLS_LoadsKeypair` | self-signed PEM → 1 cert + TLS 1.2 floor |
| `TestBuildServerTLS_RejectsBadKeypair` | missing files → error |
| `TestNewRTID_TLSEnabled` | full handshake from a TLS client succeeds (TLS ≥ 1.2 negotiated) |
| `TestNewRTID_TLSEnabled_RejectsInsecureDial` | client without trusted root fails certificate validation (proves TLS is on the wire) |

The self-signed cert is generated at test time with
`crypto/x509.CreateCertificate` over a fresh ECDSA P-256 key, scoped
to `127.0.0.1` + `localhost`, valid for 1 hour — no static fixtures
shipped.

### Manual handshake observation

```
$ openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
      -keyout /tmp/key.pem -out /tmp/cert.pem -days 1 -nodes \
      -subj "/CN=localhost" \
      -addext "subjectAltName=IP:127.0.0.1,DNS:localhost"

$ ./rtid -listen=127.0.0.1:18442 -metrics-listen=127.0.0.1:19090 \
         -tls-cert=/tmp/cert.pem -tls-key=/tmp/key.pem
{"level":"INFO","msg":"rtid: TLS enabled (clients should dial grpcs://...)"}
{"level":"INFO","msg":"rtid serving","grpc":"127.0.0.1:18442",…}

$ echo | openssl s_client -connect 127.0.0.1:18442 \
                          -CAfile /tmp/cert.pem -servername localhost
subject=CN = localhost
New, TLSv1.3, Cipher is TLS_AES_128_GCM_SHA256
Verify return code: 0 (ok)
```

Negotiated TLS 1.3, certificate validated, handshake clean.

A bare TCP probe against the same listener returns
`SSL routines:ssl3_get_record:wrong version number` — TLS is required
when enabled (no fall-back to plaintext).

A `--tls-cert` set without `--tls-key` exits at startup with the error
`rtid: --tls-cert and --tls-key must both be set` so a misconfigured
deployment does not silently fall back to insecure.

---

## Combined acceptance

```
$ go build ./...                            # clean
$ go test -race ./rti/internal/eventlog/... # ok (race test passes)
$ go test -race ./rti/cmd/rtid/...          # ok (TLS tests pass)
$ go test -race ./...                       # all green (M0–M5 stay healthy)
```

Lint: `golangci-lint run ./rti/...` reports 93 findings — identical to
the pre-change baseline (all are M0–M5 pre-existing G115 / forbidigo
items in unrelated packages). Zero new findings introduced by this PR.

## Deferred (M7 follow-ups)

- mTLS / client certificate verification.
- Cert hot-reload (currently `rtid` reads the keypair once at startup).
- Python SDK `grpcs://` branch in `pysdk/rti1516e/_transport.py` —
  Agent C territory; trivially small once the Go server side is in.
- `rti/internal/perf/baseline.go` can drop the `EventLog: nil`
  workaround — the W1B Writer is now thread-safe.
