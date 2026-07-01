# M29 Dispatch Plan — Strict HLA_EVOKED callback model (Pitch-port parity)

How the orchestrator dispatches the M29 tasks (TASK-326..334) to maximize parallel sub-agent throughput.

This document is FROZEN — only the orchestrator may edit. Companions: `docs/DISPATCH.md`, `docs/PITCH_PARITY.md` (the "Diverging" section this milestone closes), `docs/M28_DISPATCH_PLAN.md` (predecessor in the Pitch-compat track), `docs/agent-c-pysdk.md`.

---

## 1. Goal & non-goals

### Goal

Second milestone of the Pitch 1516e source-compat track. Close the **only remaining "Diverging (compatible API, different runtime behavior)" entry** in `docs/PITCH_PARITY.md` — `evokeCallback` / `evokeMultipleCallbacks` (§10.4).

Today gorti is HLA_IMMEDIATE-flavored: callback override slots (`discoverObjectInstance`, `reflectAttributeValues`, etc.) fire from the `_pump_events` background task as soon as events arrive. Pitch federates can opt for `HLA_EVOKED` at connect-time, where callbacks are buffered and only dispatch from within `evokeCallback` / `evokeMultipleCallbacks`. Code paths that depend on "no callback fires outside an evoke" (RAII-style federate runners, single-threaded simulation cores) currently misbehave on gorti.

Three parts:

1. **`CallbackModel` enum + `connect()` parameter** (W1). Per IEEE 1516.1 §4.2 + §10.4. Default `HLA_IMMEDIATE` keeps every existing M25-M28 caller on their current behavior — zero migration impact.
2. **Dispatch routing** (W1). `_dispatch_event` becomes model-aware: under HLA_EVOKED, ALWAYS buffer regardless of the `enableCallbacks`/`disableCallbacks` toggle.
3. **Strict evoke semantics** (W2). `evokeCallback` becomes at-most-one-drain under HLA_EVOKED (matching cppsdk M17.22). `evokeMultipleCallbacks` becomes drain-all-in-window under HLA_EVOKED.

W3 is the acceptance gate (new HLA_EVOKED smoke + lockfile) and docs sweep (PITCH_PARITY.md row moves from "Diverging" → "Compatible (M29)").

### Non-goals

- **C++ SDK changes.** `rti1516e::` cppsdk already has the strict at-most-one semantics in `evokeCallback` per M17.22 (see `docs/PITCH_PARITY.md:92-102`). Nothing to do on the C++ side.
- **Go SDK changes.** Not a Pitch-port target.
- **Wire-protocol changes.** Buffering happens entirely in the pysdk ambassador layer.
- **Cross-callback-model interop on the same connection.** Once `connect()` picks a model, it sticks for the connection lifetime. No mid-connection switch.
- **`enableCallbacks` / `disableCallbacks` behavior change under HLA_IMMEDIATE.** M27 Phase C semantics preserved exactly.
- **Behavior changes to `evokeCallback` / `evokeMultipleCallbacks` under HLA_IMMEDIATE.** The current "yield to the loop and observe `_callback_fired_count`" implementation stays — porting users who explicitly use HLA_IMMEDIATE see no change, M26 `test_evoke_callback.py` stays green.
- **Threading model change.** Continue running the event pump in the background loop thread. HLA_EVOKED just adds a buffering step before the override slot.

### Why now

- M28 closed the typed-handle / typed-collection gap on 2026-05-30. With that landed, `evokeCallback` is the last remaining "API compatible, runtime divergent" row in the divergence table.
- The PITCH_PARITY.md workaround (`amb.disableCallbacks()` + manual `evokeMultipleCallbacks` loop) works but is unidiomatic in ported Pitch federate code. A `connect(... callback_model=CallbackModel.HLA_EVOKED)` flag is one line in the federate.
- The buffer plumbing already exists (M27 Phase C `_callback_buffer`). M29 routes one more code path through it.

---

## 2. Surface design

### 2.1 `CallbackModel` enum (W1)

`pysdk/rti1516e/callback_model.py` (NEW):

```python
"""IEEE 1516.1 §4.2 / §10.4 callback delivery model.

HLA_IMMEDIATE — callbacks fire from the background event pump as soon
as events arrive (gorti default; matches existing M25-M28 behavior).

HLA_EVOKED — callbacks buffer until the federate calls
``evokeCallback`` or ``evokeMultipleCallbacks``. Pitch's default.
Required for federates ported from Pitch / Portico / MAK that assume
single-threaded callback delivery.
"""

from __future__ import annotations
from enum import Enum


class CallbackModel(str, Enum):
    HLA_IMMEDIATE = "HLA_IMMEDIATE"
    HLA_EVOKED = "HLA_EVOKED"
```

Re-exported from `rti1516e.__init__`.

### 2.2 `connect()` signature (W1)

```python
# pysdk/rti1516e/standard.py
def connect(
    self,
    callback_target: Rti1516eAmbassador,
    url: str,
    callback_model: CallbackModel = CallbackModel.HLA_IMMEDIATE,
) -> None: ...
```

Stored on the ambassador as `self._callback_model: CallbackModel`. Set once at `connect()`, read by `_dispatch_event` and the evoke methods. `disconnect()` doesn't reset (idempotent connect/disconnect within the same ambassador is unusual; if a user does reconnect with a different model, the new connect's value wins).

Pitch-source naming note: Pitch's Java/Python API uses `callbackModel` (camelCase). gorti follows pysdk convention with `callback_model` (snake_case). Ported federates that pass it as positional don't notice; ported federates that pass it as kwarg need a one-line rename. Documented in `docs/PITCH_PARITY.md`.

### 2.3 `_dispatch_event` routing change (W1)

Current (post-M27 C):
```python
def _dispatch_event(self, event: Any) -> bool:
    if not self._callbacks_enabled:
        self._callback_buffer.append(event)
        ...
    # fire override slot
    ...
```

M29:
```python
def _dispatch_event(self, event: Any) -> bool:
    # HLA_EVOKED: always buffer. The evoke* methods are the only
    # path that fires override slots.
    if self._callback_model is CallbackModel.HLA_EVOKED:
        self._callback_buffer.append(event)
        return True
    # HLA_IMMEDIATE: M27 Phase C enable/disable still applies.
    if not self._callbacks_enabled:
        self._callback_buffer.append(event)
        ...
    # fire override slot
    ...
```

`_callback_fired_count` is bumped ONLY when a callback is actually delivered to its override slot — not when buffered. This preserves `evokeCallback` in HLA_IMMEDIATE mode (which observes the counter to detect firings) and lets HLA_EVOKED count drains accurately.

### 2.4 `evokeCallback` strict at-most-one (W2)

```python
def evokeCallback(
    self,
    approx_min_time: float = 0.0,
    approx_max_time: float | None = None,
) -> bool:
    if self._callback_model is CallbackModel.HLA_EVOKED:
        return self._evoke_one(approx_min_time, approx_max_time)
    # HLA_IMMEDIATE — current behavior preserved.
    ...  # existing yield-and-watch implementation

def _evoke_one(self, min_t: float, max_t: float | None) -> bool:
    """Pop at-most-one buffered event and dispatch to its override.
    Returns True iff a callback was dispatched (which by Pitch
    convention also implies "more may be queued" — callers loop)."""
    if self._callback_buffer:
        event = self._callback_buffer.pop(0)
        self._dispatch_to_override(event)  # bumps _callback_fired_count
        return True
    # Buffer empty — wait up to max_t for an event to arrive.
    deadline = max_t if max_t is not None else min_t
    async def _wait() -> bool:
        elapsed = 0.0
        tick = min(0.005, max(deadline, 0.001))
        while elapsed < deadline:
            await asyncio.sleep(tick)
            if self._callback_buffer:
                event = self._callback_buffer.pop(0)
                self._dispatch_to_override(event)
                return True
            elapsed += tick
        return False
    return bool(self._run(_wait()))
```

Per the C++ SDK contract (`docs/PITCH_PARITY.md:92-102`): "EXACTLY ONE buffered callback per call. Returns true iff more events remain queued." The Python implementation matches: returns True if at-least-one dispatched (which implies the caller should loop again). Federate idiom:

```python
while amb.evokeCallback(0.0, 0.1):
    pass  # drain queue
```

### 2.5 `evokeMultipleCallbacks` drain-all-in-window (W2)

```python
def evokeMultipleCallbacks(
    self,
    approx_min_time: float = 0.0,
    approx_max_time: float | None = None,
) -> bool:
    if self._callback_model is CallbackModel.HLA_EVOKED:
        return self._evoke_drain(approx_min_time, approx_max_time)
    # HLA_IMMEDIATE — current "alias to evokeCallback" behavior.
    return self.evokeCallback(approx_min_time, approx_max_time)

def _evoke_drain(self, min_t: float, max_t: float | None) -> bool:
    """Drain ALL currently-buffered events, then wait up to max_t for
    additional events to arrive (also drained). Returns True iff at
    least one callback was dispatched."""
    fired = 0
    while self._callback_buffer:
        event = self._callback_buffer.pop(0)
        self._dispatch_to_override(event)
        fired += 1
    deadline = max_t if max_t is not None else min_t
    if deadline > 0:
        async def _wait_and_drain() -> int:
            count = 0
            elapsed = 0.0
            tick = min(0.005, max(deadline, 0.001))
            while elapsed < deadline:
                await asyncio.sleep(tick)
                while self._callback_buffer:
                    event = self._callback_buffer.pop(0)
                    self._dispatch_to_override(event)
                    count += 1
                elapsed += tick
            return count
        fired += int(self._run(_wait_and_drain()))
    return fired > 0
```

### 2.6 `enableCallbacks` / `disableCallbacks` under HLA_EVOKED (W2)

Under HLA_EVOKED, `_dispatch_event` already always buffers, so `disableCallbacks` is functionally redundant. But per IEEE 1516.1 §10.4 the toggle is orthogonal to the callback model — and `enableCallbacks` should NOT drain in HLA_EVOKED mode (only evoke* methods drain). Behavior:

- HLA_EVOKED + `_callbacks_enabled = False`: evoke* methods do NOT dispatch the buffered events (the gate is still in effect). Buffer keeps accumulating.
- HLA_EVOKED + `_callbacks_enabled = True`: evoke* methods dispatch as designed.
- `enableCallbacks` under HLA_EVOKED does NOT drain — only flips the gate.

Implementation: short-circuit the drain in `enableCallbacks` if `self._callback_model is CallbackModel.HLA_EVOKED`. The existing drain logic only applies to HLA_IMMEDIATE.

### 2.7 Helper: `_dispatch_to_override`

`_dispatch_event` currently does both "decide whether to buffer" + "invoke override". M29 splits these:

```python
def _dispatch_to_override(self, event: Any) -> bool:
    """Invoke the override slot for ``event``. Pure side-effect; no
    buffering. Bumps _callback_fired_count on success."""
    # body extracted from the bottom half of current _dispatch_event
```

`_dispatch_event` becomes the routing decision; `_dispatch_to_override` is the invocation. Both `_pump_events` (HLA_IMMEDIATE path) and `_evoke_one` / `_evoke_drain` (HLA_EVOKED paths) call `_dispatch_to_override`.

---

## 3. Acceptance criteria (exit gate)

1. **`CallbackModel` importable from `rti1516e`.** `from rti1516e import CallbackModel; assert CallbackModel.HLA_EVOKED.value == "HLA_EVOKED"`.
2. **Default model is HLA_IMMEDIATE.** An ambassador constructed with no special arg behaves identically to M28 (M26 + M28 smokes stay green).
3. **HLA_EVOKED suppresses pump-time delivery.** Under HLA_EVOKED, an event ingested by `_pump_events` is buffered, NOT delivered to override slots. Verified by `pysdk/tests/spec/m29/test_evoked_no_pump_dispatch.py`.
4. **HLA_EVOKED `evokeCallback` is at-most-one.** Two buffered events drain across two calls; each call returns True; a third call returns False. Verified by `pysdk/tests/spec/m29/test_evoke_strict.py::test_at_most_one_drain`.
5. **HLA_EVOKED `evokeMultipleCallbacks` drains all.** N buffered events all fire in one call; second call within zero-window returns False. Verified by `test_evoke_strict.py::test_drain_all`.
6. **HLA_EVOKED + `disableCallbacks` blocks evoke drain.** Buffered events stay buffered; evokeCallback returns False even with non-empty buffer when disabled. Verified by `test_evoke_strict.py::test_disabled_gates_drain`.
7. **No HLA_IMMEDIATE regression.** Full M25-M28 spec test suite passes unmodified, including `pysdk/tests/spec/m26/test_evoke_callback.py`.
8. **mypy --strict clean** across the affected files (standard.py, callback_model.py, __init__.py).
9. **Pitch-style HLA_EVOKED smoke.** New `examples/pitch-evoked-smoke/smoke_federate.py` connects with `callback_model=CallbackModel.HLA_EVOKED` and drives a full lifecycle using only evoke* for callback delivery. Smoke driver at `pysdk/tests/spec/m29/test_pitch_evoked_smoke.py`.
10. **PITCH_PARITY.md updated.** The `evokeCallback` / `evokeMultipleCallbacks` row in the divergence table moves from "Diverging" to "Compatible (M29)". The §"Diverging" prose section is rewritten to describe only the HLA_IMMEDIATE-default-vs-HLA_EVOKED-default behavior choice (which is a default policy difference, not an API divergence). The workaround pattern is replaced with a "set `callback_model=CallbackModel.HLA_EVOKED` at connect" one-liner.
11. **`docs/srs.md` §10 M29 row appended.**
12. **`CHANGELOG-MASTERPLAN.md` M29 entry landed.**
13. **`scripts/check-milestones.sh` reports `M29: DONE (N/N)`** via a new `check_m29` probe.

---

## 4. Wave structure

```
                                M29 START
                                    │
    ┌───────────────────────────────┼───────────────────────────────┐
    │                               │                               │
    │   W1   — CallbackModel enum + connect() arg + dispatch route  │
    │           pysdk/rti1516e/callback_model.py (NEW)              │
    │           pysdk/rti1516e/__init__.py re-export                │
    │           pysdk/rti1516e/standard.py:                         │
    │             __init__ stores self._callback_model              │
    │             connect() accepts kwarg                           │
    │             _dispatch_event routes by model                   │
    │             _dispatch_to_override helper extracted            │
    │                               │                               │
    │   W2   — Strict evoke semantics                               │
    │           standard.py:                                        │
    │             evokeCallback HLA_EVOKED branch (at-most-one)     │
    │             evokeMultipleCallbacks HLA_EVOKED branch (drain)  │
    │             enableCallbacks HLA_EVOKED short-circuit          │
    │                               │                               │
    │   W3   — Acceptance gate + docs                               │
    │           examples/pitch-evoked-smoke/                        │
    │           pysdk/tests/spec/m29/                               │
    │           docs/PITCH_PARITY.md row move                       │
    │           docs/srs.md M29 row + CHANGELOG +                   │
    │           scripts/check-milestones.sh check_m29 probe         │
    │                               │                               │
                                    ▼
                       M29 DONE per srs.md §10
```

W1 → W2 are sequential (W2 needs the model-aware routing W1 establishes). W3 needs both. All three waves touch `standard.py` so a single sub-agent owns the full sequence.

---

## 5. Tasks

### W1 — Model plumbing + dispatch routing
- **TASK-326**: `pysdk/rti1516e/callback_model.py` (NEW). `CallbackModel(str, Enum)` with `HLA_IMMEDIATE`, `HLA_EVOKED`. Module docstring per §2.1.
- **TASK-327**: `pysdk/rti1516e/__init__.py`: re-export `CallbackModel`. Add to `__all__`.
- **TASK-328**: `pysdk/rti1516e/standard.py`:
  - Import `CallbackModel`.
  - `__init__` stores `self._callback_model: CallbackModel = CallbackModel.HLA_IMMEDIATE`.
  - `connect()` accepts `callback_model: CallbackModel = CallbackModel.HLA_IMMEDIATE` and assigns to `self._callback_model`.
  - Extract `_dispatch_to_override` from `_dispatch_event`.
  - `_dispatch_event` routes: HLA_EVOKED always buffers; HLA_IMMEDIATE keeps M27 Phase C semantics.

### W2 — Strict evoke semantics
- **TASK-329**: `pysdk/rti1516e/standard.py`:
  - `_evoke_one` private helper (at-most-one drain + bounded wait).
  - `_evoke_drain` private helper (drain-all + bounded wait).
  - `evokeCallback` branches on model; HLA_EVOKED calls `_evoke_one`.
  - `evokeMultipleCallbacks` branches on model; HLA_EVOKED calls `_evoke_drain`.
- **TASK-330**: `pysdk/rti1516e/standard.py`:
  - `enableCallbacks` short-circuits the drain under HLA_EVOKED (only flips `_callbacks_enabled`).
  - Docstrings on evokeCallback / evokeMultipleCallbacks updated to describe both modes.

### W3 — Acceptance gate + docs
- **TASK-331**: `examples/pitch-evoked-smoke/` (NEW). `federation.fom.xml` (copy of M28's). `smoke_federate.py` mirrors `pitch-typed-smoke` but connects with `callback_model=CallbackModel.HLA_EVOKED` and drives the lifecycle via `while amb.evokeCallback(0.0, 0.1): pass` polling between actions. NO inheritance from the M28 federate — write standalone so the example is self-contained.
- **TASK-332**: `pysdk/tests/spec/m29/__init__.py` + four tests:
  - `test_evoked_no_pump_dispatch.py` — AC #3
  - `test_evoke_strict.py` (test_at_most_one_drain, test_drain_all, test_disabled_gates_drain) — AC #4, #5, #6
  - `test_pitch_evoked_smoke.py` — AC #9 (mirror of M28's smoke driver)
  - `test_callback_model_surface.py` — lockfile: CallbackModel importable + enum values + connect() accepts kwarg
- **TASK-333**: Docs sweep:
  - `docs/PITCH_PARITY.md` — move `evokeCallback` row in the divergence table; rewrite the §"Diverging" prose section per AC #10.
  - `docs/srs.md` §10 — append M29 row.
  - `CHANGELOG-MASTERPLAN.md` — M29 entry above M28 (match format).
- **TASK-334**: `scripts/check-milestones.sh` `check_m29()` probe (6 probes):
  1. `callback_model.py` exists + has `class CallbackModel`.
  2. `__init__.py` re-exports `CallbackModel`.
  3. `standard.py` has `self._callback_model` + `_dispatch_to_override` + `_evoke_one` + `_evoke_drain`.
  4. `examples/pitch-evoked-smoke/smoke_federate.py` exists + uses `HLA_EVOKED`.
  5. M29 test files exist (4 files).
  6. PITCH_PARITY.md no longer marks evokeCallback as "Diverging" (grep for the row's "Compatible (M29)" mark).
  Add `check_m29` to the dispatch list.

---

## 6. Test plan

| Test | Asserts |
|---|---|
| `test_callback_model_surface.py` | CallbackModel importable from `rti1516e`; enum values; `connect()` accepts the kwarg; `_callback_model` defaults to HLA_IMMEDIATE |
| `test_evoked_no_pump_dispatch.py` | Under HLA_EVOKED, an event arriving via the pump is buffered, NOT delivered to the override slot. Override slot fire-count stays at 0 until evokeCallback is called. |
| `test_evoke_strict.py::test_at_most_one_drain` | 3 buffered → evokeCallback returns True, True, True, False. Override called exactly 3 times. |
| `test_evoke_strict.py::test_drain_all` | 5 buffered → evokeMultipleCallbacks returns True (one call). Override called exactly 5 times. Second call returns False. |
| `test_evoke_strict.py::test_disabled_gates_drain` | HLA_EVOKED + disableCallbacks → evokeCallback returns False even with non-empty buffer. enableCallbacks; evokeCallback returns True. |
| `test_pitch_evoked_smoke.py` | Full federate lifecycle under HLA_EVOKED. Captures match expected callback payloads. |
| `pysdk/tests/spec/m26/test_evoke_callback.py` | (existing, MUST NOT REGRESS) HLA_IMMEDIATE default behavior intact. |
| `pysdk/tests/spec/m26/test_pitch_shape_smoke.py` + `m28/test_pitch_typed_smoke.py` | (existing, MUST NOT REGRESS) — M28 + M26 back-compat. |

---

## 7. Migration impact

**Zero for existing callers.** `callback_model` defaults to `HLA_IMMEDIATE` which is the M25-M28 behavior. The only behavioral change visible without opt-in is the internal refactor splitting `_dispatch_event` into `_dispatch_event` + `_dispatch_to_override` — pure code reorg, identical observable behavior.

Federates that opt in via `connect(target, url, callback_model=CallbackModel.HLA_EVOKED)`:
- Override slots stop firing from the pump thread; they fire from the calling thread of evokeCallback / evokeMultipleCallbacks.
- Callback order is preserved (FIFO from `_callback_buffer`, which is itself FIFO).
- A federate that BOTH connects with HLA_EVOKED AND forgets to call evoke* will accumulate events indefinitely in the buffer. The lockfile test documents this risk; a watchdog warning is OUT OF SCOPE.

---

## 8. M29 row append target (W3 — for reference)

```markdown
| **M29** | Agent C | Strict HLA_EVOKED callback model (Pitch-port parity) | `CallbackModel` enum (HLA_IMMEDIATE default, HLA_EVOKED opt-in); `connect()` accepts `callback_model=...`; under HLA_EVOKED `_dispatch_event` always buffers and only evoke* methods deliver to override slots; `evokeCallback` strict at-most-one drain (matches cppsdk M17.22); `evokeMultipleCallbacks` strict drain-all; `enableCallbacks`/`disableCallbacks` orthogonal to model. Closes the last "Diverging" row in `docs/PITCH_PARITY.md`. Back-compat: M25-M28 tests unmodified. **DONE 2026-MM-DD** — see `docs/M29_DISPATCH_PLAN.md` and `CHANGELOG-MASTERPLAN.md` |
```
