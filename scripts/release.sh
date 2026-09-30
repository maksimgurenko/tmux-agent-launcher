#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "$0")/.."
version=${1:?Usage: release.sh VERSION [OUTPUT_DIRECTORY]}
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || { echo 'Invalid version' >&2; exit 1; }
out=${2:-dist}
mkdir -p -- "$out"
out=$(realpath -- "$out")
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
digest=$(./scripts/build.sh --digest)
for arch in amd64 arm64; do
    stage="$tmp/$arch/tmux-agent-launcher"
    mkdir -p -- "$stage/examples"
    GOOS=linux GOARCH=$arch VERSION=$version EXPECTED_SOURCE_DIGEST=${EXPECTED_SOURCE_DIGEST:-$digest} \
        ./scripts/build.sh "$stage/tmux-agent-launcher"
    cp -- LICENSE NOTICE README.md INSTALL.agent.md CONTRIBUTING.md CONTEXT.md "$stage/"
    mkdir -p -- "$stage/scripts"
    cp -- scripts/install.sh "$stage/scripts/"
    cp -- internal/launcher/default-config.toml "$stage/examples/config.toml"
    cp -- examples/hyprland.lua "$stage/examples/"
    cp -R -- docs "$stage/docs"
    tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner \
        -C "$tmp/$arch" -czf "$out/tmux-agent-launcher-${version}-linux-${arch}.tar.gz" tmux-agent-launcher
done
(cd -- "$out" && sha256sum -- "tmux-agent-launcher-${version}-linux-"*.tar.gz > checksums.txt)
printf 'Source digest: %s\nArtifacts: %s\n' "$digest" "$out"
