# om_reserve_multi_atomic

Atomic multi-name reservation: when ANY name in the set collides,
ALL names must fail to reserve.

## Scenario

1. **Collider** federate calls `reserveObjectInstanceName(L"car-X")`
   (§6.3). RTI fires `objectInstanceNameReservationSucceeded(L"car-X")`
   on collider.
2. **Reserver** federate calls
   `reserveMultipleObjectInstanceName({L"car-Y", L"car-X", L"car-Z"})`
   (§6.5). Per spec, the operation is atomic: because `car-X` is
   already held, the entire request fails.
3. RTI fires
   `multipleObjectInstanceNameReservationFailed({L"car-X"})` on
   reserver (§6.6). The set carries ONLY the colliding subset, not
   the full requested set.
4. After the failure, `car-Y` and `car-Z` remain unreserved (atomic
   guarantee).

## Purpose

The DLC service is the singular
`reserveMultipleObjectInstanceName(set<wstring>)`. Both the succeeded
and failed callbacks take `set<wstring>`, and the failed callback reports
the colliding subset.

## Spec citations per event in goldens

### Collider

- `COLLIDER: CONNECT` — §4.2 connect
- `COLLIDER: JOIN` — §4.9 joinFederationExecution
- `COLLIDER: RESERVE_REQUEST` — §6.3 reserveObjectInstanceName
- `COLLIDER: NAME_RESERVED` — §6.3 objectInstanceNameReservationSucceeded callback
- `COLLIDER: RESIGN` — §4.10 resignFederationExecution

### Reserver

- `RESERVER: CONNECT` — §4.2 connect
- `RESERVER: JOIN` — §4.9 joinFederationExecution
- `RESERVER: MULTI_RESERVE_REQUEST` — §6.5 reserveMultipleObjectInstanceName
- `RESERVER: MULTI_RESERVED_FAILED` — §6.6 multipleObjectInstanceNameReservationFailed
- `RESERVER: RESIGN` — §4.10 resignFederationExecution

## Status

RED. Federate TUs reference `rti1516e::*` implementation symbols that don't
exist; CMake sets `WILL_FAIL TRUE`. The committed goldens contain
placeholders rather than event-level expected traces.
