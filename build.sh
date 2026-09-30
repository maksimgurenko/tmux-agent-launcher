#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
export LC_ALL=C
files=(config.go discovery.go main.go picker.go runner.go terminal.go tmux.go go.mod go.sum examples/config.toml)
digest=$(sha256sum -- "${files[@]}" | sha256sum | cut -d ' ' -f1)
if [[ ${1:-} == --digest ]]; then printf '%s\n' "$digest"; exit; fi
version=${VERSION:-development}
base=${PUBLIC_BASE:-unpublished}
expected=${EXPECTED_SOURCE_DIGEST:-$digest}
dirty=false
[[ $digest == "$expected" ]] || dirty=true
for value in "$version" "$base" "$expected"; do
    [[ $value =~ ^[A-Za-z0-9._+-]+$ ]] || { echo 'Invalid build identity' >&2; exit 1; }
done
output=${1:-build/tmux-agent-launcher}
mkdir -p -- "$(dirname -- "$output")"
CGO_ENABLED=0 go build -trimpath -buildvcs=false \
    -ldflags "-X main.version=$version -X main.publicBase=$base -X main.sourceDigest=$digest -X main.sourceDirty=$dirty" \
    -o "$output" .
