# SoftwareX submission checklist

## Required metadata

- [ ] Confirm article title and author order.
- [ ] Add each author's affiliation, email, and ORCID where applicable.
- [ ] Identify the corresponding author.
- [ ] Add funding, contributor-role, conflict-of-interest, and AI-use
      declarations required by the journal.
- [ ] Confirm the tagged software version and release date.
- [ ] Mint a source/archive DOI and update the manuscript, `CITATION.cff`, and
      `codemeta.json`.

## Repository and archive

- [ ] `main` contains the reviewed submission commit.
- [ ] CI, strict documentation build, and release workflow pass.
- [ ] Release archives contain `rtid`, `rti-top`, license, README, and checksums.
- [ ] The exact tagged source is deposited in a durable research archive.
- [ ] Claim-grade raw evidence is archived with SHA-256 checksums.
- [ ] Proprietary Pitch software and machine-local paths are excluded.

## Scientific evidence

- [ ] Semantic projection passes for all measured Pitch and gorti pairs.
- [ ] Identical FOM bytes, seed, payloads, process count, choreography,
      callbacks, logging, and measurement boundaries are attested.
- [ ] Five warmup and twenty measured pairs use balanced alternating AB/BA
      order.
- [ ] Raw sample counts, delivery accounting, medians, tails, confidence
      intervals, and order effects are retained.
- [ ] Manuscript numbers match the archived `analysis.json` exactly.
- [ ] Hardware, OS, power scheme, Go/Python/Java, and RTI versions are reported.

## Manuscript quality

- [ ] No formal certification or full vendor-equivalence claim is implied.
- [ ] Commercial RTI comparison is scoped to common observable test contracts.
- [ ] Limitations and negative optimization results are retained.
- [ ] Figures and tables have reproducible generation instructions.
- [ ] References are converted to the journal's required style.
- [ ] The final source and rendered manuscript pass spelling and link review.
