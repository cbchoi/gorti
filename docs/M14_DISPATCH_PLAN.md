# M14 Dispatch Plan — mTLS + OIDC client authentication

How the orchestrator dispatches the M14 tasks (TASK-291..310) to maximize parallel sub-agent throughput.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md`, `docs/M24_DISPATCH_PLAN.md` (predecessor), `docs/srs.md` §10.

---

## 1. Goal & non-goals

### Goal

Add transport-layer security to gorti so a production rtid can require:

1. **mTLS** — client certificates verified against a configured CA bundle. Federate must present a valid cert to make any RPC.
2. **OIDC bearer tokens** (alternative or complementary) — federate passes a JWT in gRPC metadata; server validates against a JWKS endpoint or pre-pinned signing key.

Both paths operate at the gRPC transport / interceptor layer; no service-handler changes.

### Non-goals

- **Authorization** — M14 only authenticates the federate. Per-service-call ACLs (which federate can call which RPC) are M-future scope.
- **Encryption at rest** — save bundles, event logs not in M14.
- **TLS termination at a proxy** — M14 wires native TLS in rtid; envoy/sidecars are deployment-time concerns.
- **Federate-cert-as-handle** — the federate's gRPC identity is independent of its IEEE 1516 federate handle.

### Why now

- Cut-3 production-readiness checklist requires "deployable beyond a trusted LAN." Pre-M14 every connection is plaintext + unauthenticated; any process on the network can JoinFederation.
- Reasonable scope: the gRPC ecosystem already provides TLS + interceptor primitives; M14 wires them through gorti's config + SDKs.

---

## 2. Surface design

### 2.1 Server-side flags (cmd/rtid)

New flags on `cmd/rtid`:

```
--tls-cert <path>            # Server cert (PEM)
--tls-key <path>             # Server key (PEM)
--tls-client-ca <path>       # PEM bundle to verify client certs (mTLS).
                             # Absent → TLS without client-cert verification (one-way TLS).
--tls-client-cn-allow <list> # Optional comma-separated CN allow-list. Empty → any
                             # client cert signed by --tls-client-ca passes.
--oidc-issuer <url>          # OIDC issuer URL. Server fetches JWKS from
                             # <url>/.well-known/openid-configuration.
                             # Mutually exclusive with --tls-client-ca for the
                             # auth path (OIDC OR mTLS, not both required).
--oidc-audience <aud>        # Required `aud` claim on incoming JWTs.
--oidc-jwks-pem <path>       # Pre-pinned JWKS as PEM. Fallback when no
                             # --oidc-issuer (e.g. air-gapped deployments).
```

Behavior:
- All flags absent → **insecure** (current default). Logged at startup as a WARN.
- `--tls-cert` + `--tls-key` → **TLS** (one-way). Server identity verified by clients.
- `--tls-cert` + `--tls-key` + `--tls-client-ca` → **mTLS**. Clients must present a cert.
- `--oidc-issuer` (or `--oidc-jwks-pem`) → **OIDC bearer**. Server interceptor extracts `authorization: Bearer <jwt>` from gRPC metadata and verifies.
- mTLS + OIDC are AND-stackable: if both configured, the client must satisfy both.

### 2.2 Server-side wiring

- `cmd/rtid/main.go` constructs `*tls.Config` from flags; passes to `grpc.NewServer(grpc.Creds(credentials.NewTLS(...)))`.
- New file `rti/internal/auth/oidc.go`: JWT verifier interceptor. Caches JWKS with refresh on signature mismatch.
- The interceptor injects the verified subject into the request context as `auth.SubjectFromContext(ctx)`. Service handlers may consult it for logging; M14 does not gate any handler on the subject.

### 2.3 Go SDK

New `federate.ConnectOptions`:

```go
type ConnectOptions struct {
    // TLS — server identity verification. Nil → insecure.
    TLS *tls.Config

    // BearerToken — sent as `authorization: Bearer <token>` on every RPC.
    // Combinable with TLS. Empty → no token.
    BearerToken string

    // BearerTokenProvider — alternative to BearerToken: refreshable token.
    // Called per-RPC. Empty BearerToken with non-nil Provider → Provider wins.
    BearerTokenProvider func(ctx context.Context) (string, error)
}
```

`federate.Connect(ctx, addr)` keeps its current signature for backwards compat; new `federate.ConnectWithOptions(ctx, addr, opts)` is the M14 entry point.

### 2.4 Python SDK

`RtiConnection.connect(url, *, ssl_ca=..., ssl_cert=..., ssl_key=..., bearer_token=...)`:
- `grpc://` scheme → insecure (existing behavior).
- `grpcs://` scheme → TLS. CA from `ssl_ca` bytes (if provided) or system roots.
- `ssl_cert` + `ssl_key` → mTLS.
- `bearer_token` (or callable) → metadata interceptor.

Example:
```python
async with RtiConnection.connect(
    "grpcs://rtid.example.com:8442",
    ssl_ca=open("ca.pem", "rb").read(),
    ssl_cert=open("client.crt", "rb").read(),
    ssl_key=open("client.key", "rb").read(),
    bearer_token="eyJhbGciOi...",
) as rti:
    ...
```

### 2.5 Test fixture: ephemeral CA + cert pair

`rti/internal/auth/testtls/testtls.go` — generates a self-signed CA + leaf cert + key in-memory for spec tests. Used by:
- `rti/spec/M14/mtls_test.go` — bufconn-backed mTLS round-trip.
- `pysdk/tests/spec/m14/test_mtls.py` — same shape.

### 2.6 Errors

Existing gRPC `Unauthenticated` (code 16) is the right code for both auth-failure paths. No new sentinels.

---

## 3. Acceptance criteria (exit gate)

1. **mTLS round-trip works.** Client with valid cert + valid CA chain → connects and makes RPCs. Client without cert → `Unauthenticated`. Client with cert signed by wrong CA → `Unauthenticated`.
2. **OIDC bearer token round-trip works.** Client with valid JWT → connects. Invalid signature → `Unauthenticated`. Expired → `Unauthenticated`. Missing → `Unauthenticated`.
3. **Insecure mode preserved.** Existing examples (no TLS flags) keep working.
4. **mTLS + OIDC stackable.** Server with both configured rejects clients lacking either.
5. **Python SDK round-trips both modes.**
6. **Spec test `rti/spec/M14/m14_completion_test.go` is green.**
7. **`scripts/check-milestones.sh` reports `M14: DONE`.**

---

## 4. Wave structure

W1 — server TLS + flags + cert/CA loading.
W2 — Go SDK TLS credentials + ConnectWithOptions.
W3 — Python SDK TLS + bearer token.
W4 — OIDC interceptor (server-side) + Go/Python SDK token plumbing.
W5 — spec gate + docs + check-milestones probe.

---

## 5. Tasks

- TASK-291: cmd/rtid TLS flags + tls.Config builder.
- TASK-292: gRPC server uses TLS creds when configured.
- TASK-293: Self-signed cert generator for tests (rti/internal/auth/testtls).
- TASK-294: Go SDK ConnectWithOptions + ConnectOptions.
- TASK-295: Python SDK grpcs:// scheme + ssl_* kwargs.
- TASK-296: OIDC verifier interceptor (rti/internal/auth/oidc.go).
- TASK-297: cmd/rtid OIDC flags + interceptor wiring.
- TASK-298: Go SDK bearer-token PerRPCCredentials.
- TASK-299: Python SDK bearer_token kwarg + grpc-aio call_credentials.
- TASK-300: rti/spec/M14/mtls_test.go — bufconn mTLS round-trip.
- TASK-301: rti/spec/M14/oidc_test.go — JWT verifier unit + integration tests.
- TASK-302: pysdk/tests/spec/m14/test_mtls.py + test_oidc.py.
- TASK-303: srs.md M14 row + CHANGELOG entry + check-milestones M14 probe.

---

## 6. Out of scope (explicit)

- Per-RPC authorization rules. Auth (who) without authorization (what); future cut.
- mTLS cert revocation lists / OCSP — deployment-time concerns.
- Token refresh handling beyond what `BearerTokenProvider` allows.
- Federate-cert ↔ federate-handle binding.
