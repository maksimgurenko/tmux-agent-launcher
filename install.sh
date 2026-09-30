#!/usr/bin/env bash
# Install only this launcher and its notices; never install dependencies/config.
set -euo pipefail
version= repo=maksimgurenko/tmux-agent-launcher archive= checksum= action=install update=false
prefix="$HOME/.local"
data="${XDG_DATA_HOME:-$HOME/.local/share}/tmux-agent-launcher"
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
usage() {
    cat <<'EOF'
Usage: install.sh --version vX.Y.Z [--repo OWNER/REPO] [--update]
       install.sh --archive FILE --checksum SHA256 [--update]
       install.sh --rollback | --uninstall
Options: --prefix DIR (default ~/.local), --data-dir DIR
Downloads only the selected version and checksum list. Leaves config unchanged.
EOF
}
while (($#)); do
    case $1 in
        --version|--repo|--archive|--checksum|--prefix|--data-dir)
            (($#>=2)) || die "Missing value for $1"
            case $1 in
                --version) version=$2;; --repo) repo=$2;; --archive) archive=$2;;
                --checksum) checksum=$2;; --prefix) prefix=$2;; --data-dir) data=$2;;
            esac
            shift 2;;
        --update) update=true; shift;;
        --rollback) action=rollback; shift;;
        --uninstall) action=uninstall; shift;;
        -h|--help) usage; exit;;
        *) die "Unknown option $1";;
    esac
done
[[ $prefix == /* && $data == /* ]] || die 'Installation paths must be absolute'
[[ $prefix != *$'\n'* && $data != *$'\n'* ]] || die 'Newlines in installation paths are unsupported'
bin="$prefix/bin/tmux-agent-launcher"
state="$data/.installation"
[[ ! -L $data && ! -L $state ]] || die 'Refusing symlinked installation state'
hash() { sha256sum -- "$1" | cut -d ' ' -f1; }
owned=false
if [[ -e $state ]]; then
    [[ -f $state && $(cat -- "$state") == "$bin" ]] || die 'Installation is owned by another location/manager'
    owned=true
fi
files=("$bin" "$data/LICENSE" "$data/NOTICE")
keys=(binary LICENSE NOTICE)
for key in "${keys[@]}"; do
    for suffix in sha256 previous previous.sha256; do
        [[ ! -L "$data/.$key.$suffix" ]] || die 'Refusing symlinked installation metadata/backup'
    done
done
unchanged() {
    local i=$1 path=${files[$1]} key=${keys[$1]}
    [[ -f $path && ! -L $path && -f "$data/.$key.sha256" && ! -L "$data/.$key.sha256" ]] &&
        [[ $(hash "$path") == "$(cat -- "$data/.$key.sha256")" ]]
}
if [[ $action == uninstall ]]; then
    $owned || die 'No owned installation at these paths'
    modified=false
    for i in 0 1 2; do
        if unchanged "$i"; then rm -- "${files[$i]}" "$data/.${keys[$i]}.sha256";
        elif [[ -e ${files[$i]} || -L ${files[$i]} ]]; then printf 'Preserved changed file: %s\n' "${files[$i]}"; modified=true;
        else rm -f -- "$data/.${keys[$i]}.sha256"; fi
    done
    if ! $modified; then
        rm -f -- "$state"
        for key in "${keys[@]}"; do
            if [[ -f "$data/.$key.previous" && -f "$data/.$key.previous.sha256" ]] && [[ $(hash "$data/.$key.previous") == "$(cat -- "$data/.$key.previous.sha256")" ]]; then
                rm -- "$data/.$key.previous" "$data/.$key.previous.sha256"
            elif [[ -e "$data/.$key.previous" ]]; then printf 'Preserved changed backup: %s\n' "$data/.$key.previous"; fi
        done
        rmdir -- "$data" 2>/dev/null || true
    fi
    printf 'Uninstall complete; personal config preserved.\n'; exit
fi
if $owned; then
    for i in 0 1 2; do unchanged "$i" || die "Owned file changed/missing: ${files[$i]}"; done
    for key in "${keys[@]}"; do
        if [[ -e "$data/.$key.previous" ]]; then
            [[ -f "$data/.$key.previous" && -f "$data/.$key.previous.sha256" ]] || die 'Existing backup has no ownership checksum'
            [[ $(hash "$data/.$key.previous") == "$(cat -- "$data/.$key.previous.sha256")" ]] || die 'Existing rollback backup changed; preserve/move it before updating'
        fi
    done
else
    [[ $action != rollback ]] || die 'No owned installation to roll back'
    for path in "${files[@]}"; do [[ ! -e $path && ! -L $path ]] || die "Existing unmanaged file: $path"; done
    for key in "${keys[@]}"; do
        [[ ! -e "$data/.$key.sha256" && ! -L "$data/.$key.sha256" && ! -e "$data/.$key.previous" && ! -L "$data/.$key.previous" ]] || die 'Existing installation metadata'
    done
fi
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
mkdir -p -- "$prefix/bin" "$data"
if [[ $action == rollback ]]; then
    for i in 0 1 2; do
        key=${keys[$i]}
        [[ -f "$data/.$key.previous" && ! -L "$data/.$key.previous" && -f "$data/.$key.previous.sha256" && ! -L "$data/.$key.previous.sha256" ]] || die 'No complete rollback backup'
        [[ $(hash "$data/.$key.previous") == "$(cat -- "$data/.$key.previous.sha256")" ]] || die 'Rollback backup changed'
        cp -- "$data/.$key.previous" "$tmp/$key"
    done
else
    if [[ -z $archive ]]; then
        [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.-]+)?$ ]] || die 'Choose an exact --version'
        [[ $repo =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die 'Invalid repository'
        [[ $(uname -s) == Linux ]] || die 'Only Linux is supported'
        case $(uname -m) in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; *) die 'Unsupported architecture';; esac
        asset="tmux-agent-launcher-${version}-linux-${arch}.tar.gz"
        url="https://github.com/$repo/releases/download/$version"
        curl -fLSs --proto '=https' --tlsv1.2 "$url/$asset" -o "$tmp/$asset"
        curl -fLSs --proto '=https' --tlsv1.2 "$url/checksums.txt" -o "$tmp/checksums.txt"
        checksum=$(awk -v asset="$asset" '$2 == asset {print $1}' "$tmp/checksums.txt")
        archive="$tmp/$asset"
    fi
    [[ $checksum =~ ^[a-fA-F0-9]{64}$ && -f $archive ]] || die 'Local archives require --checksum SHA256'
    [[ $(hash "$archive") == "${checksum,,}" ]] || die 'Archive checksum mismatch'
    tar -tzf "$archive" > "$tmp/list"
    for item in tmux-agent-launcher LICENSE NOTICE; do
        member="tmux-agent-launcher/$item"
        [[ $(awk -v n="$member" '$0 == n {count++} END {print count+0}' "$tmp/list") == 1 ]] || die "Archive must contain exactly one $member"
        [[ $(tar -tvzf "$archive" "$member" | cut -c1) == - ]] || die "Archive member is not a regular file: $member"
        tar -xzf "$archive" -C "$tmp" --strip-components=1 --no-same-owner --no-same-permissions -- "$member"
    done
    mv -- "$tmp/tmux-agent-launcher" "$tmp/binary"
    if $owned && [[ $(hash "$tmp/binary") != "$(cat -- "$data/.binary.sha256")" ]] && ! $update; then die 'Choose --update to replace the installed version'; fi
fi
same=true
if $owned; then
    for i in 0 1 2; do [[ $(hash "$tmp/${keys[$i]}") == "$(hash "${files[$i]}")" ]] || same=false; done
else same=false; fi
if $same; then printf 'Already installed at %s\n' "$bin"; exit; fi
# Validate all destinations before changing any, and retain the preceding set.
if $owned; then
    for i in 0 1 2; do
        key=${keys[$i]};cp -- "${files[$i]}" "$data/.$key.previous"
        hash "$data/.$key.previous" > "$data/.$key.previous.sha256"
    done
fi
for i in 0 1 2; do
    mode=0644; [[ $i != 0 ]] || mode=0755
    target=${files[$i]}
    stage=$(mktemp "$(dirname -- "$target")/.tmux-agent-launcher.XXXXXX")
    install -m "$mode" -- "$tmp/${keys[$i]}" "$stage"
    mv -T -- "$stage" "$target"
    hash "$target" > "$data/.${keys[$i]}.sha256"
done
printf '%s\n' "$bin" > "$state"
chmod 0600 "$state" "$data/".*.sha256
printf 'Installed at %s. Add %s/bin to PATH; run tmux-agent-launcher doctor.\n' "$bin" "$prefix"
