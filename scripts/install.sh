#!/bin/sh
# install.sh — fetch and install a gorti binary release.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/cbchoi/gorti/main/scripts/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/cbchoi/gorti/main/scripts/install.sh | VERSION=v0.1.0 sh
#   curl -fsSL https://raw.githubusercontent.com/cbchoi/gorti/main/scripts/install.sh | INSTALL_DIR=$HOME/.local/bin sh
#
# Env overrides:
#   VERSION            release tag to install (default: latest from GitHub API)
#   INSTALL_DIR        target directory for binaries (default: /usr/local/bin)
#   REPO               owner/repo (default: cbchoi/gorti)
#   RELEASE_BASE_URL   release-asset URL prefix, joined as
#                      "<base>/<VERSION>/<archive>" (default:
#                      https://github.com/$REPO/releases/download).
#                      Override for mirrors, internal artifact servers,
#                      or local testing with file://.
#   TMPDIR             temp scratch dir (default: mktemp -d)
#
# Supported platforms: linux/{amd64,arm64}, darwin/{amd64,arm64}.
#
# This installs rtid + rti-top only. The DDS-capable rtid-dds variant
# is not in the release tarball — build from source via `make build-dds`.

set -eu

REPO="${REPO:-cbchoi/gorti}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${VERSION:-}"

# ---------- output helpers ----------
if [ -t 1 ] && [ "${NO_COLOR:-}" = "" ]; then
    BOLD="$(printf '\033[1m')"
    DIM="$(printf '\033[2m')"
    RED="$(printf '\033[31m')"
    GRN="$(printf '\033[32m')"
    YLW="$(printf '\033[33m')"
    OFF="$(printf '\033[0m')"
else
    BOLD=""; DIM=""; RED=""; GRN=""; YLW=""; OFF=""
fi

info()  { printf '%s==>%s %s\n' "$BOLD" "$OFF" "$*"; }
ok()    { printf '%s ok%s %s\n' "$GRN" "$OFF" "$*"; }
warn()  { printf '%swarn%s %s\n' "$YLW" "$OFF" "$*" >&2; }
fail()  { printf '%serror%s %s\n' "$RED" "$OFF" "$*" >&2; exit 1; }

# ---------- platform detection ----------
detect_os() {
    uname_s="$(uname -s 2>/dev/null || echo unknown)"
    case "$uname_s" in
        Linux)  echo linux ;;
        Darwin) echo darwin ;;
        *)      fail "unsupported OS: $uname_s (gorti releases ship for linux + darwin)" ;;
    esac
}

detect_arch() {
    uname_m="$(uname -m 2>/dev/null || echo unknown)"
    case "$uname_m" in
        x86_64|amd64)        echo amd64 ;;
        aarch64|arm64)       echo arm64 ;;
        *) fail "unsupported arch: $uname_m (gorti releases ship for amd64 + arm64)" ;;
    esac
}

# ---------- tool detection ----------
have() { command -v "$1" >/dev/null 2>&1; }

pick_fetcher() {
    if have curl; then echo "curl -fsSL"
    elif have wget; then echo "wget -qO-"
    else fail "need curl or wget to download release artifacts"
    fi
}

pick_sha256() {
    if have sha256sum; then echo "sha256sum"
    elif have shasum; then echo "shasum -a 256"
    else fail "need sha256sum or shasum to verify the download"
    fi
}

# ---------- version resolution ----------
resolve_latest() {
    api_url="https://api.github.com/repos/${REPO}/releases/latest"
    # GitHub API returns JSON; extract tag_name with sed (no jq dep).
    raw="$($FETCH "$api_url")" || fail "could not query $api_url"
    tag="$(printf '%s' "$raw" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
    [ -n "$tag" ] || fail "could not parse latest release tag from $api_url"
    echo "$tag"
}

# ---------- main ----------
FETCH="$(pick_fetcher)"
SHA256="$(pick_sha256)"

OS="$(detect_os)"
ARCH="$(detect_arch)"

if [ -z "$VERSION" ]; then
    info "resolving latest release from github.com/$REPO"
    VERSION="$(resolve_latest)"
fi
ok "version: $VERSION"
ok "platform: ${OS}_${ARCH}"

# Strip leading "v" — release tarballs use 0.1.0 not v0.1.0 in their name.
VERSION_BARE="${VERSION#v}"

ARCHIVE="gorti_${VERSION_BARE}_${OS}_${ARCH}.tar.gz"
SUMS="gorti_${VERSION_BARE}_SHA256SUMS"
RELEASE_BASE_URL="${RELEASE_BASE_URL:-https://github.com/${REPO}/releases/download}"
BASE_URL="${RELEASE_BASE_URL}/${VERSION}"

WORKDIR="$(mktemp -d 2>/dev/null || mktemp -d -t gorti-install)"
cleanup() { rm -rf "$WORKDIR"; }
trap cleanup EXIT INT TERM

info "downloading $ARCHIVE"
$FETCH "${BASE_URL}/${ARCHIVE}" > "${WORKDIR}/${ARCHIVE}" \
    || fail "failed to download ${BASE_URL}/${ARCHIVE}"

info "downloading $SUMS"
$FETCH "${BASE_URL}/${SUMS}" > "${WORKDIR}/${SUMS}" \
    || fail "failed to download ${BASE_URL}/${SUMS}"

# Verify checksum. The SHA256SUMS file lists every archive; we grep the
# line that names our specific archive and feed it back through the
# sha256 tool, which exits non-zero if the file's hash doesn't match.
info "verifying SHA256"
expected_line="$(grep " ${ARCHIVE}\$" "${WORKDIR}/${SUMS}" || true)"
[ -n "$expected_line" ] || fail "no checksum entry for ${ARCHIVE} in ${SUMS}"

# Run the verifier in the workdir so the relative filename in the
# SUMS line matches the on-disk archive.
( cd "$WORKDIR" && printf '%s\n' "$expected_line" | $SHA256 -c - >/dev/null ) \
    || fail "checksum mismatch for ${ARCHIVE}"
ok "checksum verified"

info "extracting"
tar -xzf "${WORKDIR}/${ARCHIVE}" -C "$WORKDIR" rtid rti-top \
    || fail "failed to extract rtid + rti-top from ${ARCHIVE}"

# ---------- install ----------
need_sudo=""
if [ ! -d "$INSTALL_DIR" ]; then
    info "creating $INSTALL_DIR"
    if mkdir -p "$INSTALL_DIR" 2>/dev/null; then
        :
    elif have sudo; then
        sudo mkdir -p "$INSTALL_DIR" || fail "could not create $INSTALL_DIR"
        need_sudo="sudo"
    else
        fail "cannot create $INSTALL_DIR (no write permission, no sudo)"
    fi
fi

if [ -z "$need_sudo" ] && [ ! -w "$INSTALL_DIR" ]; then
    if have sudo; then
        need_sudo="sudo"
        warn "$INSTALL_DIR is not writable by $(id -un); using sudo"
    else
        fail "$INSTALL_DIR is not writable and sudo is unavailable. Set INSTALL_DIR to a writable path (e.g. \$HOME/.local/bin)"
    fi
fi

info "installing rtid + rti-top to $INSTALL_DIR"
$need_sudo install -m 0755 "${WORKDIR}/rtid"    "${INSTALL_DIR}/rtid"
$need_sudo install -m 0755 "${WORKDIR}/rti-top" "${INSTALL_DIR}/rti-top"

ok "installed: ${INSTALL_DIR}/rtid"
ok "installed: ${INSTALL_DIR}/rti-top"

# Sanity check via --version. Use the just-installed binaries directly.
case ":$PATH:" in
    *":$INSTALL_DIR:"*) on_path=1 ;;
    *)                   on_path=0 ;;
esac

printf '\n'
"${INSTALL_DIR}/rtid"    --version
"${INSTALL_DIR}/rti-top" --version

if [ "$on_path" = "0" ]; then
    printf '\n%s%s is not on PATH.%s Add it with:\n  export PATH="%s:$PATH"\n' \
        "$YLW" "$INSTALL_DIR" "$OFF" "$INSTALL_DIR"
fi
