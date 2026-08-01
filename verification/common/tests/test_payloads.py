from __future__ import annotations

import pytest

from verification.common import generate_payload, payload_envelope, verify_payload_envelope


def test_fixed_seed_payload_has_pinned_cross_language_vector() -> None:
    payload = generate_payload(40, seed=7, stream="publisher/update", index=11)
    assert payload.hex() == (
        "8d29fc082d16fbd9a515204073c192df27e99a40e44992bc6ea19e32083bc67e"
        "9a647ccd5d06b25c"
    )


def test_payload_is_random_access_and_input_separated() -> None:
    expected = generate_payload(64, seed=91, stream="OM", index=8)
    generate_payload(64, seed=91, stream="OM", index=9)

    assert generate_payload(64, seed=91, stream="OM", index=8) == expected
    assert generate_payload(64, seed=92, stream="OM", index=8) != expected
    assert generate_payload(64, seed=91, stream="TM", index=8) != expected


def test_payload_envelope_round_trip_and_integrity() -> None:
    envelope = payload_envelope(33, seed=3, stream="subscriber", index=4)
    assert verify_payload_envelope(envelope) == generate_payload(
        33, seed=3, stream="subscriber", index=4
    )

    envelope["sha256"] = "0" * 64
    with pytest.raises(ValueError, match="SHA-256"):
        verify_payload_envelope(envelope)


@pytest.mark.parametrize(
    "kwargs",
    [
        {"size": -1},
        {"size": 1, "seed": -1},
        {"size": 1, "stream": ""},
        {"size": 1, "index": -1},
    ],
)
def test_payload_rejects_invalid_coordinates(kwargs: dict[str, object]) -> None:
    with pytest.raises(ValueError):
        generate_payload(**kwargs)  # type: ignore[arg-type]
