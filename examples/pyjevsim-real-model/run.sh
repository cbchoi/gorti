#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${here}/../.." && pwd)"

resolve_python() {
  local candidates=()
  local candidate
  local resolved

  [[ -n "${PYTHON:-}" ]] && candidates+=("${PYTHON}")
  if [[ -n "${RTID_VENV:-}" ]]; then
    candidates+=("${RTID_VENV}/bin/python" "${RTID_VENV}/Scripts/python.exe")
  fi
  candidates+=(
    "${repo_root}/.venv/bin/python"
    "${repo_root}/.venv/Scripts/python.exe"
    python3
    python
  )

  for candidate in "${candidates[@]}"; do
    if [[ -f "${candidate}" ]]; then
      resolved="${candidate}"
    else
      resolved="$(command -v "${candidate}" 2>/dev/null || true)"
    fi
    if [[ -n "${resolved}" ]] &&
      "${resolved}" -c 'import sys; raise SystemExit(sys.version_info < (3, 11))' \
        >/dev/null 2>&1; then
      printf '%s\n' "${resolved}"
      return 0
    fi
  done
  return 1
}

python_bin="$(resolve_python)" || {
  echo "pyjevsim-real-model: Python 3.11 or newer was not found. Set PYTHON or RTID_VENV." >&2
  exit 1
}

for arg in "$@"; do
  if [[ "${arg}" == "-h" || "${arg}" == "--help" ]]; then
    exec "${python_bin}" "${here}/runner.py" "$@"
  fi
done

"${python_bin}" "${here}/preflight.py" || exit 1

exec "${python_bin}" "${here}/runner.py" "$@"
