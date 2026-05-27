#!/usr/bin/env bash
set -euo pipefail

version="${1:-snapshot}"
case "$version" in
  *[!A-Za-z0-9._-]*|"")
    echo "usage: $0 [version]" >&2
    echo "version may contain only letters, numbers, dot, underscore, and dash" >&2
    exit 2
    ;;
esac

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out_dir="$root/dist/$version"
staging_dir="$out_dir/staging"
cache_dir="${GOCACHE:-$out_dir/.gocache}"
commit="unknown"
if git -C "$root" rev-parse --short=12 HEAD >/dev/null 2>&1; then
  commit="$(git -C "$root" rev-parse --short=12 HEAD)"
fi
if [[ "${SOURCE_DATE_EPOCH:-}" != "" ]]; then
  build_date="$(date -u -d "@$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ)"
else
  build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi
ldflags=(
  "-s"
  "-w"
  "-buildid="
  "-X"
  "github.com/chinaykc/atm/pkg/app/cli.Version=$version"
  "-X"
  "github.com/chinaykc/atm/pkg/app/cli.Commit=$commit"
  "-X"
  "github.com/chinaykc/atm/pkg/app/cli.Date=$build_date"
)

targets=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)

rm -rf "$out_dir"
mkdir -p "$staging_dir" "$cache_dir"

for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  name="atm_${version}_${goos}_${goarch}"
  package_dir="$staging_dir/$name"
  binary="atm"
  if [[ "$goos" == "windows" ]]; then
    binary="atm.exe"
  fi

  mkdir -p "$package_dir"
  (
    cd "$root"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOCACHE="$cache_dir" go build \
      -trimpath \
      -buildvcs=false \
      -ldflags="${ldflags[*]}" \
      -o "$package_dir/$binary" \
      ./cmd/atm
  )

  cp "$root/LICENSE" "$root/README.md" "$root/README.zh-CN.md" "$root/SECURITY.md" "$package_dir/"
  (
    cd "$staging_dir"
    zip -X -q -r "$out_dir/$name.zip" "$name"
  )
done

(
  cd "$out_dir"
  sha256sum *.zip > checksums.txt
)

rm -rf "$staging_dir"
if [[ "${GOCACHE:-}" == "" ]]; then
  rm -rf "$cache_dir"
fi

echo "release artifacts:"
find "$out_dir" -maxdepth 1 -type f -print | sort
