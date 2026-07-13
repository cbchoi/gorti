# SoftwareX submission package

This directory contains the editable SoftwareX manuscript and submission
checklist for gorti. It does not represent an accepted or published article.

## Files

- `softwarex.tex`: manuscript in the mandatory SoftwareX original-software
  LaTeX structure.
- `softwarex.md`: longer editable source and evidence notes.
- `highlights.txt`: short contribution highlights for the submission system.
- `submission-checklist.md`: metadata, archive, evidence, and declaration work
  that must be complete before submission.

## Release procedure for the paper

1. Complete every author and declaration field in the checklist.
2. Run the repository and documentation verification suites.
3. Create an immutable release tag and source archive.
4. Archive the claim-grade comparison directory separately, including raw
   logs, manifests, checksums, and `analysis.json`.
5. Mint the archive DOI and add it to the manuscript, `CITATION.cff`, and
   `codemeta.json`.
6. Regenerate the final manuscript artifact from the tagged source.

The journal currently requires the official template, a 4,000-word limit,
C1-C8 code metadata, five named main sections, and an editable `.tex` or
`.docx` source. Recheck the Guide for Authors immediately before submission.
The current official original-software template is linked from the
[SoftwareX Guide for Authors](https://www.sciencedirect.com/journal/softwarex/publish/guide-for-authors);
download it again before final rendering so journal-owned template updates are
not hidden in a vendored copy.

Do not commit local Pitch installation paths or proprietary Pitch binaries and
logs to the public source repository.
