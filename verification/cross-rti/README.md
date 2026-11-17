# Reusing Java service cases with another RTI

This experiment compiles three existing gorti Java test classes against either
gorti or Portico. It changes provider selection and connection setup, not service
assertions. It is a service-test transfer, not a performance comparison or an
assessment of complete RTI conformance.

## Cases

| Existing class | Invocations | Observations attempted |
|---|---:|---|
| `LiveFederationTest` | 1 | Federation lifecycle, name errors, time enablement and peer-dependent grants |
| `LiveExchangeTest` | 2 | Object/interaction exchange and metadata; synchronization and evoked dispatch |
| `LiveTimeArithmeticTest` | 2 | GALT and representable positive-lookahead bounds at time 1 and 2^53 |

The original sources remain under
[`javasdk/client/src/test/java/org/gorti/rti`](../../javasdk/client/src/test/java/org/gorti/rti).
`run.py` generates copies and records their hashes and complete adaptation diffs.
It removes the gorti-endpoint test-enablement condition and four gorti URI checks,
selects the provider through `TestRti`, and retains all HLA calls and assertions.
The FOM is copied unchanged. Failed early assertions prevent later assertions in
that invocation from running; reports must not imply coverage of the whole class.

`PortableInteractionControl` is a separate, newly written control. It sends one
receive-order interaction and checks the class, parameter count, four payload
bytes, tag, receive order, and one delivery within a bounded observation window.
It does not substitute for, or turn failures into passes in, the original cases.

## Environment and setup

- Python 3.11+, a JDK with `java` and `javac`, and JUnit Platform Console 1.11.4.
- [JUnit artifact](https://repo.maven.apache.org/maven2/org/junit/platform/junit-platform-console-standalone/1.11.4/junit-platform-console-standalone-1.11.4.jar),
  SHA-256 `b016ef6b1c3454d6d7c2c88ce081dabf289699686af6622d6e4e2e1b54b4a2fc`.
- For Portico: an independently installed Portico 2.1.4 `lib/portico.jar`.
- For gorti: build the server and Java client from the same checkout.

Run from the repository root. Commands below use external output directories;
replace the paths for your machine. On Windows use `python` and `run.ps1`; on
Linux use `python3` and `bash verification/cross-rti/run.sh` with the same flags.

```powershell
go build -o D:/results/rtid.exe ./rti/cmd/rtid
mvn -f javasdk/pom.xml -Dmaven.test.skip=true install
mvn -f javasdk/client/pom.xml dependency:build-classpath -Dmdep.outputFile=D:/results/java-dependencies.txt

python verification/cross-rti/run.py --implementation gorti --junit-console D:/tools/junit-platform-console-standalone-1.11.4.jar --gorti-classpath-file D:/results/java-dependencies.txt --rtid D:/results/rtid.exe --output D:/results/service-gorti
python verification/cross-rti/run.py --implementation portico --junit-console D:/tools/junit-platform-console-standalone-1.11.4.jar --portico-jar D:/tools/portico/lib/portico.jar --output D:/results/service-portico
```

On Linux, build `rtid` without the `.exe` suffix and use an absolute path such as
`/tmp/gorti-results/java-dependencies.txt` for Maven's classpath output. The client
JAR is added automatically from `javasdk/client/target`; `--gorti-client-jar`
overrides it. Maven dependency paths use the current platform's separator.
`--select` runs one class; omit it to run the five retained invocations and the
separate control. Each output directory must be new.

Each class runs in a fresh JVM. Its participants are separate RTI ambassador
sessions in that JVM, matching the original fixtures; they are not separate
federate OS processes. gorti also uses a separate server process. Both providers
use `HLA_EVOKED`. Portico uses a generated JGroups TCP/TCPPING resource on IPv4
loopback; no Portico implementation classes are modified. The discovery list
covers 16 local ports because channels can bind when joining, after multiple
ambassadors have connected. gorti uses its default synchronous callback-reporting
build, not the asynchronous performance build.

## Results and publication

See [the observed results](RESULTS.md). Published report copies, generated test
sources, adaptation diffs, and exact commands are under [`results/`](results/).
They include the failed discovery attempt as well as the corrected run. JUnit
XML contains the first failure and stack trace for each failed invocation.
Run `python verification/cross-rti/summarize.py` to check public file hashes and
recount JUnit outcomes without installing either RTI.

`export_reports.py` replaces local paths/host identifiers and removes unrelated
user-name and system-library-path metadata. Each run's publication manifest gives
the original and public file hashes. This is a path-redacted copy of retained
reports, not regenerated execution evidence. Runtime JARs and executables are not
redistributed. Newly generated runs stay outside Git until deliberately reviewed
for publication; published examples are explicitly selected evidence.

The Linux launch command is supplied for reproduction. The recorded cross-RTI
experiment was run on Windows 11 with Temurin 11.0.31; no Linux execution of this
new transfer is claimed.
