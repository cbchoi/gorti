# gorti

Open-source [IEEE 1516-2010 (HLA Evolved)](https://standards.ieee.org/ieee/1516/4118/) Run-Time Infrastructure in Go, with a spec-strict IEEE 1516.1-2010 DLC C++ federate SDK, a Python federate SDK, and a [pyjevsim](https://github.com/cbchoi/pyjevsim) DEVS bridge.

**Status (v0.9.0)**: full HLA Evolved service surface with a CI-enforced
conformance program — 27/27 conformance fixtures at FULL/SPEC-FULL
(canonicalized event sequences byte-identical to their goldens; the 3
goldens capturable under Pitch pRTI Free's 2-federate cap were captured
from a live Pitch 5.5.10 and match byte-for-byte, the rest are derived
from the IEEE spec text). See [`docs/PITCH_PARITY.md`](docs/PITCH_PARITY.md)
for the scoreboard and its honest caveats, and
[`CHANGELOG-MASTERPLAN.md`](CHANGELOG-MASTERPLAN.md) for the milestone
history (M0 walking skeleton → M38/M39 conformance + SDK-parity close).

What's in the box:
- **`rtid`** — Go RTI server: federation lifecycle, declaration + object
  management, time management (TAR/TARA/NER/NMRA/FQR, strict TSO order,
  §8.8 next-message grants), ownership (real §7.6 two-phase divest, atomic
  §7.9 acquire-if-available), DDM with scope advisories, MOM (HLAmanager
  instance fan-out + HLAreport interactions), save/restore, sync points,
  message retraction, event log, and reproducible determinism
  (byte-identical event logs across runs).
- **`cppsdk`** — IEEE 1516.1-2010 **DLC C++ API** (`RTI/…` headers,
  `rti1516e` namespace): source-compatible with federates written for any
  DLC-conformant RTI. Locked by ~200 compile-time lockfile assertions, 121
  runtime-tested Annex C exceptions, and 27 behavioral conformance fixtures.
- **`rti1516e` (pysdk)** — Python federate SDK with idiomatic asyncio API
  (Layer 1) and a 1516-shaped ambassador adapter (Layer 2); typed handles,
  full FederateEvent coverage, Annex-C-named exceptions.
- **`pyjevsim_bridge`** — DEVS↔HLA bridge so pyjevsim coupled models join
  HLA federations.
- **Cross-language byte-identical encoding** — Python, Go, and C++ agree on
  every entry of `tests/conformance/encoding_vectors.json`; proven live by
  the `xlang_python_cpp_pubsub` fixture (Python publisher → C++ subscriber).
- **Conformance CI** — `scripts/ci-gates.sh` re-runs every fixture against
  a fresh RTI on each commit and fails on any verdict change in either
  direction (see `cppsdk/tests/dlc/conformance/EXPECTED_VERDICTS.txt`),
  plus an IVCT-inspired Python subset (35 tests, 0 xfail).

**Choosing gorti vs a commercial RTI** — honest guidance: gorti is a strong
fit for C++/Python federates, development/CI (deterministic replay, no
federate cap, headless), research, and education. It is **not yet** a
substitute where you need a Java federate SDK (none exists here), certified
IVCT compliance (our subset is IVCT-*inspired*; see
`tests/conformance/rti/ivct-subset/README.md`), large-scale distributed
federations (cluster/failover code is demo-grade; single-node is the
supported mode), or legacy HLA 1.3 / 1516-2000 interfaces.

## Quickstart

### Install from a release (no Go toolchain required)

One-liner — fetches the latest release, verifies the SHA256, installs `rtid` + `rti-top` to `/usr/local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/cbchoi/gorti/main/scripts/install.sh | sh
```

Pin a version or override the install directory via env vars:

```bash
# Pin a specific tag
curl -fsSL https://raw.githubusercontent.com/cbchoi/gorti/main/scripts/install.sh | VERSION=v0.1.0 sh

# Install to ~/.local/bin (no sudo required)
curl -fsSL https://raw.githubusercontent.com/cbchoi/gorti/main/scripts/install.sh | INSTALL_DIR=$HOME/.local/bin sh
```

Supported platforms: `linux_amd64`, `linux_arm64`, `darwin_amd64`, `darwin_arm64`. The script uses `curl` or `wget`, and `sha256sum` or `shasum -a 256` — whichever is available.

Prefer to do it by hand? Download a tarball + the matching `gorti_<version>_SHA256SUMS` from the [releases page](https://github.com/cbchoi/gorti/releases) and `tar -xz` the binaries.

The release tarball is CGo-free (statically linked), so it has no runtime system dependencies. The DDS-capable `rtid-dds` variant is **not** in the release tarball — it requires Cyclone DDS and is built from source via `make build-dds` (see [M19 design doc](docs/m19-dds-adapter.md)).

### Run the Go reference example

```bash
git clone https://github.com/cbchoi/gorti
cd gorti
go run ./examples/go-pingpong              # 1000 ping-pong interactions, in-process
go run ./examples/go-timed -ticks=10       # 3 federates with NER over 10 logical ticks
```

### Run the Python+RTI cross-language example

```bash
# Install Python deps (Python 3.11+)
cd pysdk && pip install -e '.[dev]' pyjevsim==2.0.1
make py-codegen                            # regenerate gRPC stubs into _generated/

# Two-Python smoke against a real rtid (subprocess-spawned)
cd ..
python3 examples/pyjevsim/runner.py        # in-process via FakeRtiServer
pytest pysdk/tests/spec/m5/test_spec_m5_cross_language.py  # cross-process via gRPC
```

### Build rtid yourself

```bash
go build -o bin/rtid ./rti/cmd/rtid
./bin/rtid --listen :8442 --metrics-listen :9090
```

Federate connects with `grpc://localhost:8442`. (TLS: `--tls-cert/--tls-key` pinned for production; M6 hardening track.)

### DDS data plane (opt-in, M19)

The default `rtid` binary uses gRPC for both control + data planes.
A build-tag-gated DDS data plane is opt-in for federations that need
DDS's discovery / multicast properties — see
[`docs/m19-dds-adapter.md`](docs/m19-dds-adapter.md) for the design
and `make build-dds` for the DDS-capable variant. M19 is a multi-phase
deliverable; Phase 1a (foundation, no CGo) has landed; Phase 1b adds
the actual Cyclone DDS interop.

## Using `rtid`

### Listener model

`rtid` opens up to three TCP listeners. They are independent — bind any of them to `0.0.0.0` only when you understand the auth posture (see "Security caveats" below).

| Port | Flag | Default | Purpose |
|---|---|---|---|
| Federate | `--listen` | `:8442` | gRPC for federates (`grpc://` or `grpcs://`). |
| Admin | `--admin-listen` | `localhost:8443` | Read-only `AdminService` (Snapshot / TailEvents / Status) — backs `rti-top`. Loopback by default. Empty string disables. |
| Metrics | `--metrics-listen` | `:9090` | Prometheus scrape endpoint at `/metrics`. |

### Run a server

```bash
# Plaintext (dev / trusted network)
rtid --listen :8442 --log-dir ./eventlogs --save-dir ./gorti-saves

# TLS (federates dial grpcs://host:8442)
rtid --listen :8442 \
     --tls-cert ./certs/server.pem --tls-key ./certs/server.key \
     --log-dir ./eventlogs

# Best-effort federation mode (lower latency, no per-federate ack accounting)
rtid --federation-mode best-effort
```

`--log-dir` is required for any federation that should persist its event log to disk — leave empty and the federation will refuse to start. `--save-dir` is the on-disk root for federation save bundles (M9, FR-SR-1..5).

### Demo modes (no federate code required)

`rtid --mode=` runs an in-process federation against itself for smoke testing:

```bash
rtid --mode=pingpong-demo --pingpong-rounds 1000 --log-dir ./eventlogs
rtid --mode=timed-demo    --timed-ticks 100      --log-dir ./eventlogs
rtid --mode=replay-from-log --replay-input ./eventlogs/<federation>.eventlog
```

Pair `--pingpong-deterministic` / `--timed-deterministic` with the same `--log-dir` across runs to get byte-identical event logs (the determinism harness in `make determinism`).

### Useful flags

| Flag | Purpose |
|---|---|
| `--version` | Print version + commit + build date and exit. |
| `--log-level` | `debug`/`info`/`warn`/`error` (default `info`). |
| `--log-format` | `json` (default) or `text`. |
| `--federation-mode` | `verbose` (default; ack-accounted) or `best-effort`. |
| `--research-config` | TOML file selecting alternative LBTS/Grant/Negotiation strategies (see [`docs/research-platform-howto.md`](docs/research-platform-howto.md)). |
| `--admin-mutating` | Enable `ForceResign`/`DestroyFederation` on the admin port. Refuses non-loopback bind unless `--admin-mutating-allow-non-loopback=true`. |

Full flag list: `rtid --help`.

### Security caveats

- The federate port supports server-side TLS, and mTLS + OIDC client auth
  (M14) for authenticated deployments — plaintext `--listen` remains a
  trusted-network port.
- `--admin-listen` is plaintext. Keep it on loopback; if you must expose it, front it with an ACL.
- `--admin-mutating` performs irreversible operations (force-resign, destroy-federation). Off by default; the binary refuses to start with `--admin-mutating` on a non-loopback bind unless you explicitly opt in.

## Writing a federate

### Python (idiomatic, supported)

`rti1516e` is the supported federate SDK. Install from the source tree:

```bash
cd pysdk && pip install -e '.[dev]' pyjevsim==2.0.1
make py-codegen   # one-time: generates gRPC stubs into pysdk/rti1516e/_generated/
```

A minimal federate that joins, publishes one interaction, advances logical time, and resigns:

```python
import asyncio
from rti1516e.connection import RtiConnection, FederationSpec

async def main() -> None:
    spec = FederationSpec(
        name="demo",
        fom_modules=["./fom/HLAstandardMIM.xml", "./fom/demo.xml"],
        mode="verbose",
    )
    async with RtiConnection.connect("grpc://localhost:8442") as rti:
        async with rti.join_federation(spec, federate_name="alice") as fed:
            await fed.publish_interaction_class("HLAinteractionRoot.Ping")
            await fed.subscribe_interaction_class("HLAinteractionRoot.Ping")

            await fed.enable_time_regulation(lookahead=1.0)
            await fed.enable_time_constrained()

            await fed.send_interaction("HLAinteractionRoot.Ping", {"seq": 1})
            await fed.next_message_request(time=10.0)
            # ... drain grants/events from fed.events() ...

asyncio.run(main())
```

Connect URLs:

- `grpc://host:port` — plaintext.
- `grpcs://host:port` — TLS; pass `ca_cert=Path("ca.pem").read_bytes()` to `connect()` for a private CA.
- `memory://name` — in-process driver for tests (no network, deterministic).

Cut-3 service groups (`fed.sync`, `fed.ownership`, `fed.ddm`, `fed.savepoint`, `fed.mom`) are lazy properties — use them only when you need those service surfaces. Reference federates live under [`examples/`](examples/) (`pyjevsim`, `pyjevsim-relay`, `pyjevsim-time-advance`, `pyjevsim-sync-points`).

### Go (raw gRPC)

There is no idiomatic Go federate SDK — the in-tree Go examples (`go-pingpong`, `go-timed`) drive `rtid` in `--mode=pingpong-demo`/`timed-demo`. External Go federates connect by generating gRPC stubs from [`proto/rti/v1/*.proto`](proto/rti/v1/) directly. The proto contracts are orchestrator-frozen; cross-language handle alignment landed at M6.

## Observing with `rti-top`

`rti-top` is a top-style TUI that polls `rtid`'s read-only AdminService.

```bash
# rtid running with default --admin-listen=localhost:8443
rti-top                                     # 1 Hz refresh, default address
rti-top --rtid-addr remote.example:8443     # remote daemon (loopback default; expose with care)
rti-top --refresh 250ms                     # 100 ms .. 60 s
```

Keybindings:

| Key | Action |
|---|---|
| `f` | Federations view (roster) |
| `o` | drill down into the selected federation |
| `t` | Time view — LBTS sparkline + grants |
| `w` | Wire view — per-RPC rate / sort / column-toggle (`s` / `c`) |
| `i` | Events view — `TailEvents` stream |
| `f` (in Events view) | filter input |
| `p` (in Events view) | pause / resume the stream |
| `/` | filter the current table |
| `↑`/`↓` or `j`/`k` | move selection |
| `enter` | drill into selection · `esc` step back |
| `r` | cycle refresh interval (100 ms ↔ 60 s) |
| `q` or `ctrl+c` | quit |
| `x` / `d` | (mutating-only) ForceResign federate / DestroyFederation |

Mutating keys (`x`, `d`) only appear when `rtid` was started with `--admin-mutating=true`; they're hidden by default.

## Performance

Reference baseline ([`docs/reports/M5/agent-a.md`](docs/reports/M5/agent-a.md), 12th Gen i7-12700, 20 cores, 10s/size, in-process):

| Federation size | Throughput (interactions/sec) | p50 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 2   | 3,035,333 |  0.044 |  0.125 |
| 5   | 1,253,641 | 10.16  | 15.04  |
| 25  |   184,191 |  2.90  | 23.22  |
| 100 |    48,863 |  3.22  | 33.81  |

Reproduce: `go run -tags=perf ./rti/cmd/perf-baseline`.

## Repo layout

| Path | Purpose |
|---|---|
| `proto/rti/v1/` | gRPC contracts (orchestrator-frozen) |
| `rti/` | Go RTI server + encoder + FOM parser |
| `pysdk/` | Python federate SDK + pyjevsim bridge |
| `examples/` | Reference federates (go-pingpong, go-timed, pyjevsim) |
| `tests/conformance/` | Cross-language conformance vectors + FOM fixtures |
| `tests/spec/M1/`, `rti/spec/M{2,3,5}/`, `pysdk/tests/spec/m{4,5}/` | Per-milestone specification tests (orchestrator-frozen) |
| `docs/` | SRS, SDD, IDD, agent briefs, dispatch plans, status reports |
| `CHANGELOG-MASTERPLAN.md` | Full milestone-by-milestone history |

## Standards conformance

IEEE 1516-2010 (HLA Evolved) only — not 1516-2000, not 1.3, not HLA 4.

- **API**: the C++ SDK implements the IEEE 1516.1-2010 DLC header surface
  (`RTI/…`, 30 headers) verbatim-compatible; drift is locked by
  compile-time assertion TUs under `cppsdk/tests/dlc/lockfile/`.
- **Behavior**: 27 conformance fixtures exercise every service group
  end-to-end against a live `rtid`; verdicts are canonicalized-event
  comparisons against goldens (3 captured from Pitch pRTI 5.5.10, the
  rest spec-derived) and enforced by CI on every commit.
- **Known divergences and scope-outs** are documented, not hidden:
  [`docs/PITCH_PARITY.md`](docs/PITCH_PARITY.md) (scoreboard + caveats),
  [`docs/DLC_DIVERGENCE_CATALOGUE.md`](docs/DLC_DIVERGENCE_CATALOGUE.md),
  [`docs/RTI_CONFORMANCE_AUDIT.md`](docs/RTI_CONFORMANCE_AUDIT.md).
- Exceptions carry a machine-readable channel (gRPC trailing metadata
  `rti-spec-exception` = Annex C class name) — the contract for
  third-party SDK authors is in `cppsdk/src/dlc/README.md`.

## For contributors

Read the design and operating manuals in this order:

1. [`docs/srs.md`](docs/srs.md) — Software Requirements Specification
2. [`docs/sdd.md`](docs/sdd.md) — Software Design Document
3. [`docs/idd.md`](docs/idd.md) — Interface Design Document
4. [`docs/AGENTS.md`](docs/AGENTS.md) — operating manual (architecture + role decomposition)
5. [`docs/ORTHOGONALITY.md`](docs/ORTHOGONALITY.md) — path-ownership table (per-file owner)
6. [`docs/DISPATCH.md`](docs/DISPATCH.md) — task-dispatch protocol
7. [`docs/TDD.md`](docs/TDD.md) — test-first commit pattern
8. [`docs/WORKFLOW.md`](docs/WORKFLOW.md) — git + PR rules
9. [`docs/CODING_CONVENTIONS.md`](docs/CODING_CONVENTIONS.md) — strict rules (no `time.Now()`, sorted iteration, etc.)
10. `docs/M{2,3,4,5}_DISPATCH_PLAN.md` — per-milestone wave models (reference for M6+ planning)

## Build & test

```bash
make verify       # fmt + lint + test + determinism (run before any PR)
make fmt          # gofmt + goimports + ruff --fix
make lint         # golangci-lint + ruff + buf lint
make test         # go test + pytest
make determinism  # 10x determinism harness on core packages
make proto        # regenerate Go gRPC bindings (requires buf)
make py-codegen   # regenerate Python gRPC bindings (requires grpcio-tools)
make py-test      # pytest pysdk/
make py-lint      # ruff check pysdk/
make py-typecheck # mypy --strict pysdk/
```

Milestone status: `./scripts/check-milestones.sh` (auto-detects M0..M5 state from the working tree).

## License

MIT — see [`LICENSE`](LICENSE).
