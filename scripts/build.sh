#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "$0")/.."
export LC_ALL=C
files=(cmd/tmux-agent-launcher/main.go internal/launcher/config.go internal/launcher/discovery.go internal/launcher/cli.go internal/launcher/picker.go internal/launcher/runner.go internal/launcher/terminal.go internal/launcher/tmux.go go.mod go.sum internal/launcher/default-config.toml)
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
    -ldflags "-X github.com/maksimgurenko/tmux-agent-launcher/internal/launcher.version=$version -X github.com/maksimgurenko/tmux-agent-launcher/internal/launcher.publicBase=$base -X github.com/maksimgurenko/tmux-agent-launcher/internal/launcher.sourceDigest=$digest -X github.com/maksimgurenko/tmux-agent-launcher/internal/launcher.sourceDirty=$dirty" \
    -o "$output" ./cmd/tmux-agent-launcher
