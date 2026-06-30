# Pitch pRTI Free EULA review — golden-file capture licensing

**Status:** DRAFT review. Owner: orchestrator. Prerequisite for TASK-350 (M31 → M32 W2 golden capture). This document is NOT legal advice; it is an honest engineering reading of the EULA the orchestrator can act on while a legal review is pending.

**Subject:** Whether checking captured event-log output from federate programs that link Pitch headers into the gorti repo (under MIT) is permitted under the Pitch pRTI Free EULA.

**Source documents:**
- `~/prti1516e/docs/eula.txt` (50 lines, dated 6 September 2012). Full text below the analysis.
- `~/prti1516e/.install4j/i4jparams.conf` (install-time params).

---

## TL;DR — Conclusion

**LIKELY PERMITTED** to commit canonicalized federate-program output captured from a Pitch-CRC-served federation, subject to four conditions:

1. **No Pitch software is redistributed.** The repo contains only the captured stdout/stderr text of a federate program *I wrote* that happened to be linked against Pitch headers and connected to a Pitch CRC. The Pitch binary, headers, JARs, license files, and trademarks are NOT in the repo. The user runs Pitch locally under their own EULA.
2. **No Pitch-emitted lines are checked in.** Goldens are scrubbed of (a) the Pitch banner, (b) the Pitch CRC startup log, (c) any line not under the federate's `SUB:` / `PUB:` prefix. The `grep "^SUB:" pitch.sub.log` pattern from the 2026-06-30 smoke is the template.
3. **No Pitch trademarks/logos appear in committed files.** Per EULA §5. Goldens carry an attribution like `// captured-via: Pitch pRTI Free 5.5.10 (test reference only)` but DO NOT use Pitch wordmarks or pRTI logo art.
4. **The repo's use case is "learning and testing," not commercial development.** Per EULA §1. gorti is MIT-licensed open-source RTI development — research/learning/testing. Documented as such in `README.md` and `LICENSE`. Commercial users of gorti are subject to the Pitch EULA's commercial-development clause if they install Pitch to run the parity-mode leg locally; that's on them, not gorti.

If a follow-on legal review disagrees, the fallback (per `docs/DLC_COMPLIANCE_PROGRAM.md §5.6` last bullet) is to **switch goldens to hand-authored from spec text only** — slower, less accurate, but unambiguously gorti-owned. Either path keeps M31's RED scaffold intact since M31 ships `expected.*.log` skeletons with `// TBD-pitch-capture` markers regardless.

---

## Risk analysis

### Clause-by-clause read of the EULA

**§1 Limited License Grant.** "Pitch hereby grants to Licensee a non-exclusive, non-transferable, limited license to use the Software, in machine readable form only, for Licensee's learning and testing purposes only. It may not be used for commercial development."

- gorti development is learning/research. ✓
- The repo does not redistribute the Software. ✓
- The repo's MIT license does NOT extend to Pitch. If a commercial consumer of gorti uses gorti's parity-mode harness against Pitch to develop commercial software, they need their own Pitch commercial license (or to skip the parity leg). That is documented in `cppsdk/tests/dlc/conformance/_harness/README.md`.

**§2 License Restrictions.** "Licensee shall not copy, alter, modify or adapt the Software or any part thereof... Licensee shall not translate, reverse engineer, decompile, disassemble, decrypt, extract or create derivative works of or from the Software or any part thereof."

- "Derivative works of or from the Software" is the central concern. A captured stdout log of `federate.cpp` (gorti-owned source) is at the legal margin:
  - **Argument it is NOT a derivative work:** the log text is output of MY federate's `printf` calls. The federate happens to be linked against Pitch headers, but Pitch's library merely provides the runtime services (object registration, callback delivery) — it does not author the federate's output strings. By analogy, a screenshot of a program running under macOS is not a derivative work of macOS.
  - **Argument it MIGHT be:** the *sequence* and *timing* of callbacks reflects Pitch's internal scheduling, and the log values (handles, ordering) are determined by Pitch's runtime. A determined plaintiff could argue the log reveals Pitch implementation behavior. This is the weaker leg.
- **Mitigation:** Goldens canonicalize handles → `<H>`, sort RO bucket events, strip wall-clock. After canonicalization, only spec-mandated information (which events fire, in which TSO order, with which payloads) remains — and that is **spec-defined behavior**, not Pitch-specific. A correct Pitch and a correct gorti must produce identical canonicalized goldens; differences are bugs or spec ambiguities. Therefore the canonical goldens reveal only spec-defined behavior, not Pitch internals.

**§3 Ownership.** "The Software is protected by trade secret, copyright and other proprietary rights..."

- The Software (binaries, headers, JARs) is not in the repo. ✓
- The captured output text is not a copy of the Software. The arrangement we propose is: federate-source-under-MIT + spec-defined-event-sequence-captured-as-text. Both sides are gorti's.

**§4 Confidentiality.** "Licensee shall keep the Software confidential, except for any information that is (a) generally available or known to the public..."

- The Software is not committed; confidentiality of the Software is preserved. ✓
- The captured output reveals only spec-defined event sequences after canonicalization. Spec-defined behavior is "generally available" via the published IEEE 1516.1-2010 standard. ✓

**§5 Trademarks and Logos.** "This Agreement does not authorize Licensee to use any Pitch name, trademark or logo."

- Goldens carry attribution as comments, not Pitch wordmarks. Example: `// captured against Pitch pRTI Free 5.5.10 build 9905 (test reference only — does not imply Pitch endorsement)`. ✓
- README/docs that mention Pitch use plain text "Pitch pRTI" descriptively, not stylized marks. ✓

**§6 Disclaimer of Intended Use.** Aviation/nuclear restrictions. ✓ (Not relevant; gorti is generic simulation infrastructure.)

**§7-13.** Warranty disclaimer, liability cap, termination, assignment, governing law (Swedish), severability, entire agreement. None of these restrict golden-file capture.

### Boundary cases worth flagging

1. **A Pitch update changes its event sequence.** The captured goldens become stale and the parity diff fails. This is an engineering problem (re-capture) not a licensing one.
2. **A consumer of gorti runs the parity harness commercially.** Their use of Pitch is governed by their own Pitch EULA. gorti's repo does not change their obligations.
3. **A user without Pitch installed tries to run parity tests.** `pitch_build.sh` exits with code 1 (PRTI_HOME unset) and the parity leg is skipped. ✓

---

## Recommended workflow

**Authoring a golden** (in M32+, after M31's stubs ship):

1. Run the federate against gorti rtid first; capture `gorti.log`.
2. Canonicalize via `cppsdk/tests/dlc/conformance/_harness/normalize.py`.
3. Compare against the spec section the fixture cites. Hand-author the golden from the spec text; do NOT just copy `gorti.log`.
4. **Optionally** validate against Pitch: run the federate against Pitch CRC under `pitch_run.sh`; canonicalize; diff. If gorti and Pitch agree, that's bake-off evidence the spec sentence is interpreted the same way both directions. If they diverge, apply the tie-breaker rule (`docs/DLC_COMPLIANCE_PROGRAM.md §5.2.2`).
5. Add the `// §N.M` traceability cite in the fixture's `README.md` per `scripts/check-spec-traceability.sh`.

The committed golden's authority is the spec sentence cited in the README, **not** the Pitch run that helped author it.

**Authoring boundaries:**

- Goldens MUST cite a spec § for every event.
- Goldens MUST be canonicalized (handles → `<H>` etc).
- Goldens MUST NOT include any Pitch banner/copyright text.
- Goldens SHOULD NOT include raw timestamps or wall-clock data.

If a fixture's golden has lines that match `grep -E 'pRTI|Pitch|Copyright.*Pitch|version 5'`, the lint fails. (TODO: add this to `scripts/check-spec-traceability.sh` in a follow-on.)

---

## Open questions for legal review

1. Does Swedish law's "derivative work" definition (governing law per §11) materially differ from US/EU readings such that the canonicalized-log argument fails?
2. If Pitch reads §2 as broadly as "any output produced by federate code that exercised the Software", do they have a track record of enforcement against open-source projects? (Unknown; if Pitch has been silent on similar uses by Portico/CERTI/etc., that's weakly favorable.)
3. Does the §4 confidentiality clause survive the §1 "learning and testing" grant in a way that restricts publishing canonical event sequences derived from such use?

None of these are blockers for M31 (which lands `// TBD-pitch-capture` skeletons regardless). They become blockers in M32 W2 when real captures need to commit.

---

## Decision

**Tentative GO** for committing canonicalized goldens captured via Pitch CRC, subject to the four conditions in the TL;DR and the workflow above. Re-review with a lawyer before any commit of an actual golden if the project takes external contributions that touch the goldens. The fallback (hand-author from spec only) is documented and feasible; no critical path depends on the Pitch-assisted capture.

**M31 impact:** none — M31's `expected.*.log` files are skeletons with `// TBD-pitch-capture` markers; the WILL_FAIL flag is set per fixture; M31 acceptance does not depend on real goldens. This document UNBLOCKS the M32 W2 golden capture.

---

## Appendix — Full EULA text

Pitch pRTI(tm) Free Edition End User License Agreement
Version 6 September 2012
==============================================================

This End User License Agreement, including all appendices attached hereto, ("Agreement") sets forth the terms and conditions relating to the licensing by Pitch Technologies AB, ("Pitch") of the Pitch pRTI Free software and documentation ("Software").
By installing, copying or otherwise using the Software (the earliest of such acts constituting the effective date of this Agreement), you ("Licensee") agree to be bound by the terms of this Agreement.

1. Limited License Grant.
Pitch hereby grants to Licensee a non-exclusive, non-transferable, limited license to use the Software, in machine readable form only, for Licensee's learning and testing purposes only. It may not be used for commercial development.

Furthermore, Licensee may:
 (i) Install and use one copy of the Central RTI Component ("CRC") on a single computer.
 (ii) Install and use a maximum of two (2) local RTI components ("LRC") on computer that connect to a licensed CRC.

2. License Restrictions.
Licensee shall not copy, alter, modify or adapt the Software or any part thereof, except that Licensee may make one archival copy of the Software for backup use.  Licensee shall not translate, reverse engineer, decompile, disassemble, decrypt, extract or create derivative works of or from the Software or any part thereof. Licensee shall not remove or modify any proprietary markings or restrictive legends placed on the Software. The Software is licensed, not sold. Licensee shall not sublicense, re-lease, transfer or distribute the Software, whether by license, loan, rental, sale or otherwise in whole or in part.
3. Ownership.
Licensee acknowledges and agrees that the Software is protected by trade secret, copyright and other proprietary rights, and that any and all such rights to and in the Software vest and shall remain vested in Pitch and its licensors. Pitch reserves all rights not expressly granted herein.

4. Confidentiality.
Licensee shall keep the Software confidential, except for any information that is (a) generally available or known to the public, (b) disclosed through no act or omission of Licensee or any of its employees or agents, (c) lawfully disclosed to Licensee by a third party not under any confidentiality obligation, and (d) disclosed as required by a court or similar tribunal.

5. Trademarks and Logos.
This Agreement does not authorize Licensee to use any Pitch name, trademark or logo. Licensee acknowledges that Pitch owns the pRTI trademark, all pRTI related trademarks, logos and icons ("pRTI Marks"), and agrees to: (i) not do anything harmful to or inconsistent with Pitch's rights in the pRTI Marks, and (ii) assist Pitch in protecting those rights, including assigning to Pitch any rights acquired by Licensee in any pRTI Marks.

6. Disclaimer of Intended Use.
Licensee expressly acknowledges and agrees that the Software is intended only to assist in modeling and/or simulation. The Software is not designed or intended for any other purposes, including - but not limited to - use (a) in control of aircraft, air traffic, aircraft navigation or aircraft communications, or (b) for the design, construction, operation or maintenance of any nuclear facility.  Licensee shall not use the Software in any such application or for any such purpose.

7. Disclaimer of Warranty.
Software is provided "AS IS", without a warranty of any kind.

8. Limitation of Liability. [...trim — full text in source file...]

9. Termination. [...]

10. Assignment. [...]

11. Governing Law and disputes. This Agreement shall be governed by Swedish laws.

12. Severability. [...]

13. Entire Agreement. [...]

(Full text at `~/prti1516e/docs/eula.txt`.)
