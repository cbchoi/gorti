#!/usr/bin/env bash
# Statically validate the cross-platform entrypoints in each example directory.
set -uo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
EXAMPLES_DIR="$REPO_ROOT/examples"
FAILURES=()

fail() {
  FAILURES+=("$1")
  printf 'ERROR: %s\n' "$1" >&2
}

code_without_line_comments() {
  LC_ALL=C grep -Ev '^[[:space:]]*#' "$1" || true
}

has_machine_path() {
  code_without_line_comments "$1" |
    LC_ALL=C grep -Eq '(^|[^[:alnum:]])[[:alpha:]]:[\\/]|/(home|Users)/[^[:space:]]+'
}

if [[ ! -d "$EXAMPLES_DIR" ]]; then
  printf 'ERROR: examples directory not found at %s\n' "$EXAMPLES_DIR" >&2
  exit 1
fi

shopt -s nullglob
example_dirs=("$EXAMPLES_DIR"/*/)
example_count=0

for example_dir in "${example_dirs[@]}"; do
  example_name="$(basename -- "$example_dir")"
  if [[ "$example_name" == '__pycache__' ]]; then
    continue
  fi
  example_count=$((example_count + 1))
  readme="$example_dir/README.md"
  shell_entrypoint="$example_dir/run.sh"
  powershell_entrypoint="$example_dir/run.ps1"

  for required in "$readme" "$shell_entrypoint" "$powershell_entrypoint"; do
    if [[ ! -f "$required" ]]; then
      fail "examples/$example_name/$(basename -- "$required") is missing"
    elif [[ ! -s "$required" ]]; then
      fail "examples/$example_name/$(basename -- "$required") is empty"
    fi
  done

  if [[ -s "$shell_entrypoint" ]]; then
    if [[ ! -x "$shell_entrypoint" ]]; then
      fail "examples/$example_name/run.sh must be executable (Git mode 100755)"
    fi
    IFS= read -r first_line < "$shell_entrypoint" || true
    if [[ "$first_line" != '#!/usr/bin/env bash' ]]; then
      fail "examples/$example_name/run.sh must start with #!/usr/bin/env bash"
    fi
    if LC_ALL=C grep -q $'\r' "$shell_entrypoint"; then
      fail "examples/$example_name/run.sh must use LF line endings"
    fi
    if ! bash -n "$shell_entrypoint" >/dev/null 2>&1; then
      fail "examples/$example_name/run.sh has invalid Bash syntax"
    fi
    if ! code_without_line_comments "$shell_entrypoint" |
      LC_ALL=C grep -Eq 'BASH_SOURCE|\$0'; then
      fail "examples/$example_name/run.sh must resolve paths from its own location"
    fi
    if ! code_without_line_comments "$shell_entrypoint" |
      LC_ALL=C grep -Eq '^[[:space:]]*set[[:space:]]+-[^[:space:]]*e'; then
      fail "examples/$example_name/run.sh must enable fail-fast mode with set -e"
    fi
    if code_without_line_comments "$shell_entrypoint" |
      LC_ALL=C grep -Eiq '^[[:space:]]*(exec[[:space:]]+)?(pwsh|powershell)(\.exe)?([[:space:]]|$)'; then
      fail "examples/$example_name/run.sh must not invoke PowerShell"
    fi
    if has_machine_path "$shell_entrypoint"; then
      fail "examples/$example_name/run.sh contains a machine-specific absolute path"
    fi
  fi

  if [[ -s "$powershell_entrypoint" ]]; then
    if ! code_without_line_comments "$powershell_entrypoint" |
      LC_ALL=C grep -Eiq '\$PSScriptRoot|\$MyInvocation\.MyCommand\.(Path|Definition)'; then
      fail "examples/$example_name/run.ps1 must resolve paths from its own location"
    fi
    if ! code_without_line_comments "$powershell_entrypoint" |
      LC_ALL=C grep -Eiq "^[[:space:]]*\\\$ErrorActionPreference[[:space:]]*=[[:space:]]*['\"]Stop['\"]"; then
      fail "examples/$example_name/run.ps1 must set ErrorActionPreference to Stop"
    fi
    if code_without_line_comments "$powershell_entrypoint" |
      LC_ALL=C grep -Eiq '^[[:space:]]*(&[[:space:]]+)?(bash|sh)(\.exe)?([[:space:]]|$)'; then
      fail "examples/$example_name/run.ps1 must not invoke a POSIX shell"
    fi
    if has_machine_path "$powershell_entrypoint"; then
      fail "examples/$example_name/run.ps1 contains a machine-specific absolute path"
    fi
  fi
done

if (( example_count == 0 )); then
  fail "no immediate example directories found under examples/"
fi

if (( ${#FAILURES[@]} > 0 )); then
  printf 'check-example-entrypoints: FAILED (%d issue(s))\n' "${#FAILURES[@]}" >&2
  exit 1
fi

printf 'check-example-entrypoints: OK (%d example(s))\n' "$example_count"
