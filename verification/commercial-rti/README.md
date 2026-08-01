# Commercial RTI verification boundary

This directory contains a vendor-neutral IEEE 1516-2010 verification client.
It intentionally contains no commercial RTI binary, product name, installation
path, proprietary configuration key, sample source, or server launcher.

The public part of the verifier provides:

- one Java federate implementation for FM, DM, OM, and TM scenarios;
- the canonical two-federate FOM;
- deterministic semantic and performance record formats;
- the fair-comparison result builder; and
- a local-launcher contract for a licensed RTI installation.

## Local configuration

Set these values outside the repository:

```powershell
$env:REFERENCE_RTI_API_JAR = 'C:\absolute\path\to\ieee1516e-api.jar'
$env:REFERENCE_RTI_JAVA = 'C:\absolute\path\to\java.exe'
$env:REFERENCE_RTI_LAUNCHER = 'C:\absolute\path\to\licensed-rti-launcher.ps1'
```

`REFERENCE_RTI_JAVA` is optional when `java` is already on `PATH`. The API JAR
and launcher are required. A provider-specific launcher may be stored under
`verification/commercial-rti/local/`; that directory and `*.local.ps1` files
are ignored by Git.

The launcher receives these neutral command-line options:

```text
--verifier-jar <absolute path>
--api-jar <absolute path>
--java <absolute path>
--fom <absolute path>
--seed 1516
--count <integer>
--server-event-log off|file
--output-directory <absolute path>
--federation-name <name>
--run-id <id>
--timeout-ms <integer>
--server-address <address>             # optional
--workload-contract <absolute path>    # fair runs
```

The local launcher owns all licensed-product operations: locating libraries,
configuring the RTI executive, starting or selecting a server, and terminating
processes it started. It must launch two independent verifier JVM processes and
write the following artifacts into `--output-directory`:

- `canonical.ndjson`: normalized FM, DM, OM, and TM observations;
- `benchmark.json`: samples using `gorti.production-benchmark/v1`; and
- `run-evidence.json`: sealed process, runtime, and log evidence using
  `gorti.commercial-rti/run-evidence-v1`.

When launching `CommercialRtiVerifier`, the local adapter may pass its
implementation-defined IEEE local settings designator as
`--local-settings-designator <value>`. The public verifier does not assume an
address syntax, server name, or default port.

Attested `run-evidence.json` files describe `runtime_artifacts` with
`server_artifact`, `ieee1516e_api_jar`, and `verifier_jar`; they also capture
the server process, publisher/subscriber JVM processes, and stdout/stderr
artifacts with byte counts and SHA-256 hashes.

Raw vendor logs, binaries, API libraries, installer metadata, and license text
must remain outside version control.

## Build

```powershell
.\Build.ps1 -ApiJar $env:REFERENCE_RTI_API_JAR
```

The output is `build\reference_rti-verifier.jar`.

## Run

```powershell
.\Run.ps1 `
  -Fom .\fom\CommercialRtiVerifier.xml `
  -Seed 1516 `
  -Count 100
```

For balanced AB/BA measurement, use `FairRun.ps1` through
`verification/fair-comparison`. Both RTI arms must use the same FOM bytes,
seed, process count, choreography, callback mode, logging mode, warm-up count,
measurement count, and measurement boundaries.

## Scope

The resulting evidence supports only the retained two-federate scenarios. It
does not claim wire compatibility, product certification, unrestricted scale,
or equivalence for untested HLA services. Public reports identify the other
implementation only as `reference_rti`.
