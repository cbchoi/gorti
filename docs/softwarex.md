# SoftwareX submission

The `paper/` directory contains a submission-oriented SoftwareX manuscript and
checklist. It describes the software architecture, supported HLA services,
quality controls, interoperability evidence, reproducibility protocol,
limitations, and future work.

The manuscript is a draft, not an acceptance or certification claim. Before
submission, the authors must add journal-specific author affiliations,
declarations, repository/archive DOI, and the exact release tag used for the
paper.

## Reproduction package

The source repository contains the scripts, FOM, schemas, workload contract,
and semantic validators needed to reproduce the comparison. Generated outputs
are excluded from Git because they contain large logs and machine-specific
paths. A submission archive should pair the tagged source release with the
complete claim output directory and checksum both artifacts.

See [reproducibility](reproducibility.md), [verification](verification.md), and
[performance](performance.md) for the public documentation.
