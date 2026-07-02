#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$script_dir"

if ! command -v qshell >/dev/null 2>&1; then
  echo "qshell is required. Install it from https://github.com/qiniu/qshell/releases." >&2
  exit 127
fi

for arg in "$@"; do
  case "$arg" in
    -h | --help)
      exec qshell sandbox template build "$@"
      ;;
  esac
done

if [[ -z "${QINIU_API_KEY:-}" ]]; then
  echo "warning: QINIU_API_KEY is not set; qshell may fail authentication." >&2
fi

if [[ "${AONE_API_URL:-}" == http://localhost* ]]; then
  echo "warning: AONE_API_URL points to localhost; qshell template builds use qshell.sandbox.toml and Qiniu auth." >&2
fi

exec qshell sandbox template build \
  --name playground \
  --dockerfile ./Dockerfile \
  --path . \
  --cpu 4 \
  --memory 8192 \
  --wait \
  "$@"
