#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
role=""
endpoint="${GORTI_ENDPOINT:-}"
maven="${GORTI_MAVEN:-mvn}"
maven_repo="${GORTI_MAVEN_REPO:-}"
federation="java-exchange"
timeout="30"

usage() {
    printf '%s\n' 'Usage: bash examples/java-exchange/run.sh --role publisher|receiver --endpoint grpc://host:port [options]' \
        '  --federation NAME  Shared federation name (default: java-exchange)' \
        '  --maven PATH       Maven executable (default: GORTI_MAVEN or mvn)' \
        '  --maven-repo PATH  Local Maven cache (default: GORTI_MAVEN_REPO or Maven default)' \
        '  --timeout SECONDS  Protocol wait budget, 1-300 (default: 30)' \
        'Use grpcs://host:port for TLS. Install the SDK once and start the RTI server separately.'
}

while (($#)); do
    case "$1" in
        --help|-h) usage; exit 0 ;;
        --role|--endpoint|--maven|--maven-repo|--federation|--timeout)
            if (($# < 2)); then printf 'Missing value for %s\n' "$1" >&2; exit 2; fi
            case "$1" in
                --role) role="$2" ;;
                --endpoint) endpoint="$2" ;;
                --maven) maven="$2" ;;
                --maven-repo) maven_repo="$2" ;;
                --federation) federation="$2" ;;
                --timeout) timeout="$2" ;;
            esac
            shift 2 ;;
        *) printf 'Unknown option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
    esac
done

case "$role" in
    publisher|receiver) ;;
    *) printf '%s\n' 'Specify --role publisher or --role receiver.' >&2; exit 2 ;;
esac
case "$endpoint" in
    grpc://*:*|grpcs://*:*) ;;
    *) printf '%s\n' 'Specify --endpoint grpc://host:port or grpcs://host:port (or GORTI_ENDPOINT).' >&2; exit 2 ;;
esac
if [[ ! "$timeout" =~ ^[1-9][0-9]{0,2}$ ]] || ((10#$timeout > 300)); then
    printf '%s\n' '--timeout must be between 1 and 300 seconds.' >&2
    exit 2
fi
if [[ -z "$federation" || -z "$maven" ]]; then
    printf '%s\n' '--federation and --maven must not be empty.' >&2
    exit 2
fi
fom="$script_dir/exchange-fom.xml"
if [[ ! -f "$fom" ]]; then printf 'FOM file not found: %s\n' "$fom" >&2; exit 2; fi
common=('-B' '-ntp')
if [[ -n "$maven_repo" ]]; then
    if [[ "$maven_repo" != /* ]]; then maven_repo="$PWD/$maven_repo"; fi
    common+=("-Dmaven.repo.local=$maven_repo")
fi

exec "$maven" "${common[@]}" '-f' "$script_dir/../../javasdk/client/pom.xml" 'exec:java' \
    '-Dexec.mainClass=org.gorti.examples.ExchangeFederate' \
    "-Dgorti.role=$role" "-Dgorti.endpoint=$endpoint" "-Dgorti.federation=$federation" \
    "-Dgorti.fom=$fom" "-Dgorti.timeoutSeconds=$timeout"
