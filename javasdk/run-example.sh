#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
endpoint="${GORTI_ENDPOINT:-}"
fom="${GORTI_FOM:-$script_dir/../examples/go-tar-wait/tar-wait-fom.xml}"
maven="${GORTI_MAVEN:-mvn}"
maven_repo="${GORTI_MAVEN_REPO:-}"
timeout=10

usage() {
    printf '%s\n' 'Usage: bash javasdk/run-example.sh --endpoint grpc://host:port [options]' \
        '  --fom PATH         FOM XML (default: examples/go-tar-wait/tar-wait-fom.xml)' \
        '  --maven PATH       Maven executable (default: GORTI_MAVEN or mvn)' \
        '  --maven-repo PATH  Local Maven cache (default: GORTI_MAVEN_REPO or Maven default)' \
        '  --timeout SECONDS  Callback timeout, 1-300 (default: 10)' \
        'Use grpcs://host:port for TLS. Start the RTI server separately.'
}

while (($#)); do
    case "$1" in
        --help|-h) usage; exit 0 ;;
        --endpoint|--fom|--maven|--maven-repo|--timeout)
            if (($# < 2)); then printf 'Missing value for %s\n' "$1" >&2; exit 2; fi
            case "$1" in
                --endpoint) endpoint="$2" ;;
                --fom) fom="$2" ;;
                --maven) maven="$2" ;;
                --maven-repo) maven_repo="$2" ;;
                --timeout) timeout="$2" ;;
            esac
            shift 2 ;;
        *) printf 'Unknown option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
    esac
done

case "$endpoint" in
    grpc://*:*|grpcs://*:*) ;;
    *) printf '%s\n' 'Specify --endpoint grpc://host:port or grpcs://host:port (or GORTI_ENDPOINT).' >&2; exit 2 ;;
esac
if [[ ! "$timeout" =~ ^[1-9][0-9]{0,2}$ ]] || ((10#$timeout > 300)); then
    printf '%s\n' '--timeout must be between 1 and 300 seconds.' >&2
    exit 2
fi
if [[ ! -f "$fom" ]]; then printf 'FOM file not found: %s\n' "$fom" >&2; exit 2; fi
fom="$(cd -- "$(dirname -- "$fom")" && pwd)/$(basename -- "$fom")"
common=(-B -ntp)
if [[ -n "$maven_repo" ]]; then
    if [[ "$maven_repo" != /* ]]; then maven_repo="$PWD/$maven_repo"; fi
    common+=("-Dmaven.repo.local=$maven_repo")
fi

"$maven" "${common[@]}" -f "$script_dir/pom.xml" -DskipTests install
"$maven" "${common[@]}" -f "$script_dir/client/pom.xml" exec:java \
    -Dexec.mainClass=org.gorti.examples.TimeFederate \
    "-Dgorti.endpoint=$endpoint" "-Dgorti.fom=$fom" "-Dgorti.timeoutSeconds=$timeout"
