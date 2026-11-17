# gorti verification and benchmark evidence r1

Data-only release of retained test reports, model traces, benchmark trials,
execution settings, and requirements-mapping corrections. This is not a new
software version and does not replace gorti 1.0.0 or its software DOI.

Download **gorti-softwarex-evidence-r1.zip** from the assets below. GitHub's
automatic source archives are repository snapshots, not the raw evidence package.

- Size: 72,380,707 bytes; 1,979 files.
- SHA-256: `f41baa119b20c2a90015c676684dfc37a022019ec52d99acf701c93d5dd37ed2`.
- Includes 75 historical test reports, nine model runs, and three separate
  performance campaigns containing 300 trials including warm-ups.
- Includes the historical and corrected mappings, selected test sources,
  original/public hashes, and an explicit redaction ledger.
- Does not include runtime binaries, IEEE PDFs, manuscript/reviewer PDFs,
  credentials, private path maps, or student records.

After extraction, from `gorti-softwarex-evidence-r1` with Python 3.12+:

```text
python -B verify_package.py
python -B reanalyze.py --output reanalysis-output
```

The archive was checked using Python 3.14.0. These commands verify and reanalyze
retained observations; they do not execute a new RTI experiment. Unresolved
requirements, skipped invocations, separate performance sessions, and limitations
of source provenance are retained. No whole-standard conformance or general
performance ranking is claimed.

Publication was authorized on 18 November 2026. Preparation-time statements
inside the approved ZIP saying "candidate", "not published", or authorization
pending are historical. This release records the subsequent publication without
altering that archive's bytes. Existing rights notices are retained; publication
does not create a blanket license over third-party material.

See the [evidence guide](https://github.com/cbchoi/gorti/blob/evidence-r1/verification/evidence-r1/README.md)
for result interpretation and repository copies of analysis code and settings.
The later [cross-RTI service transfer](https://github.com/cbchoi/gorti/blob/evidence-r1/verification/cross-rti/README.md)
is separate from the historical campaigns in this ZIP.
