# om_reserve_multi_atomic

Atomic multi-name reservation: when ANY name in the set collides,
ALL names must fail to reserve. M31 dispatch plan §2.2 fixture #9.

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

## Why this fixture

Divergence catalogue row 11.1 (BLOCKING): gorti M17 has plural
`reserveMultipleObjectInstanceNames(vector<string>)`. DLC requires
singular `Name` with `set<wstring>`. Row 4.18 (BLOCKING): both
succeeded/failed callbacks take `set<wstring>` (M17 had `vector<string>`
+ a 2-arg failed form).

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
- `RESERVER: MULTI_RESERVED_FAILED` — §6.6 multipleObjectInstanceNameReservationFailed (catalogue row 4.18)
- `RESERVER: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Federate TUs reference `rti1516e::*` impl symbols that don't
exist; CMake `WILL_FAIL TRUE` per dispatch plan §3 criterion 2.
Goldens are `TBD-pitch-capture` placeholders until Agent E's TASK-363
(Pitch EULA review) clears.
