"""Deterministic Plan-Do-Review-Reflect gate for improvement.md."""

from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from dataclasses import asdict, dataclass
from pathlib import Path


@dataclass(frozen=True)
class PhaseResult:
    phase: str
    score: int
    checks: dict[str, bool]


def _contains_all(text: str, terms: Sequence[str]) -> bool:
    return all(term in text for term in terms)


def evaluate(text: str) -> list[PhaseResult]:
    plan = {
        "measurable_hypotheses": _contains_all(text, ("가설", "수치 목표", "production workload")),
        "measurement_contract": _contains_all(
            text, ("측정 계약", "20 measured runs", "raw per-operation")
        ),
        "risk_and_rollback": _contains_all(text, ("위험 및 롤백", "feature flag", "fallback")),
    }
    do = {
        "prioritized_stages": _contains_all(text, ("P0", "P1-A", "P1-B", "P2-A", "P3")),
        "production_decomposition": _contains_all(
            text, ("Transport decomposition", "TAR-only", "Logging factorial")
        ),
        "evidence_artifacts": _contains_all(
            text, ("before/after raw data", "CPU/mutex/block profile", "decision record")
        ),
    }
    review = {
        "statistical_gate": _contains_all(
            text, ("bootstrap 95% 신뢰구간", "ratio CI width", "p95", "p99")
        ),
        "delivery_accounting": _contains_all(text, ("expected_fanout", "dropped=0", "duplicate")),
        "semantic_gate": _contains_all(
            text, ("canonical semantic log", "event log bytes", "Go race")
        ),
        "resource_gate": _contains_all(text, ("CPU", "allocations", "RSS", "goroutine")),
    }
    reflect = {
        "explicit_decisions": _contains_all(text, ("Accept", "Rework", "Reject")),
        "learning_updates_work": _contains_all(
            text, ("예상과 달랐던 결과", "backlog", "남은 위험")
        ),
        "automatic_rejection": _contains_all(
            text, ("자동 거부 조건", "single-run", "sent-only", "benchmark-only")
        ),
    }

    def score(checks: dict[str, bool]) -> int:
        passed = sum(checks.values())
        if passed == len(checks):
            return 2
        return 1 if passed else 0

    return [
        PhaseResult("plan", score(plan), plan),
        PhaseResult("do", score(do), do),
        PhaseResult("review", score(review), review),
        PhaseResult("reflect", score(reflect), reflect),
    ]


def run(document: Path, output_dir: Path, max_iterations: int) -> dict[str, object]:
    output_dir.mkdir(parents=True, exist_ok=True)
    text = document.read_text(encoding="utf-8")
    iterations: list[dict[str, object]] = []
    decision = "rework"

    for number in range(1, max_iterations + 1):
        phases = evaluate(text)
        total = sum(item.score for item in phases)
        by_phase = {item.phase: item.score for item in phases}
        approved = total >= 7 and by_phase["plan"] == 2 and by_phase["review"] == 2
        decision = "accept" if approved else "rework"
        record = {
            "iteration": number,
            "phases": [asdict(item) for item in phases],
            "total_score": total,
            "decision": decision,
        }
        iterations.append(record)
        (output_dir / f"iteration-{number:03d}.json").write_text(
            json.dumps(record, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
        )
        if approved:
            break

    result: dict[str, object] = {
        "document": str(document),
        "max_iterations": max_iterations,
        "iterations": iterations,
        "final_decision": decision,
    }
    (output_dir / "summary.json").write_text(
        json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    parser.add_argument("--document", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--max-iterations", type=int, default=3)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.max_iterations < 1:
        raise SystemExit("--max-iterations must be at least 1")
    result = run(args.document.resolve(), args.output_dir.resolve(), args.max_iterations)
    last = result["iterations"][-1]
    print(json.dumps(last, ensure_ascii=False, indent=2))
    return 0 if result["final_decision"] == "accept" else 1


if __name__ == "__main__":
    raise SystemExit(main())
