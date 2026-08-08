#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
script="$repo_root/scripts/verify-single-host-recording.sh"

if [[ ! -x "$script" ]]; then
  echo "expected executable verifier at $script" >&2
  exit 1
fi

help=$("$script" --help)
[[ "$help" == *"--segments-dir"* ]]
[[ "$help" == *"--database"* ]]
[[ "$help" == *"--skip-build"* ]]
echo "verify-single-host-recording command contract: PASS"
