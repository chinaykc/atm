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
  build_date="$(python3 -c 'from datetime import datetime, timezone; import os; print(datetime.fromtimestamp(int(os.environ["SOURCE_DATE_EPOCH"]), timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
else
  build_date="$(python3 -c 'from datetime import datetime, timezone; print(datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
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

if [[ "${ATM_RELEASE_TARGETS:-}" != "" ]]; then
  read -r -a targets <<< "$ATM_RELEASE_TARGETS"
else
  targets=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
    "windows/arm64"
  )
fi

zip_dir() {
  local source_dir="$1"
  local zip_path="$2"
  python3 - "$source_dir" "$zip_path" <<'PY'
import os
import sys
import zipfile

source_dir = os.path.abspath(sys.argv[1])
zip_path = os.path.abspath(sys.argv[2])
root = os.path.dirname(source_dir)

with zipfile.ZipFile(zip_path, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    for dirpath, dirnames, filenames in os.walk(source_dir):
        dirnames.sort()
        filenames.sort()
        rel_dir = os.path.relpath(dirpath, root).replace(os.sep, "/")
        if rel_dir != ".":
            info = zipfile.ZipInfo(rel_dir.rstrip("/") + "/")
            info.external_attr = (0o755 << 16) | 0x10
            archive.writestr(info, b"")
        for filename in filenames:
            path = os.path.join(dirpath, filename)
            arcname = os.path.relpath(path, root).replace(os.sep, "/")
            archive.write(path, arcname)
PY
}

write_sha256() {
  local file="$1"
  local out="$2"
  python3 - "$file" "$out" <<'PY'
import hashlib
import os
import sys

path = sys.argv[1]
digest = hashlib.sha256()
with open(path, "rb") as handle:
    for chunk in iter(lambda: handle.read(1024 * 1024), b""):
        digest.update(chunk)
with open(sys.argv[2], "w", newline="\n") as handle:
    handle.write(f"{digest.hexdigest()}  {os.path.basename(path)}\n")
PY
}

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
  zip_dir "$package_dir" "$out_dir/$name.zip"
  write_sha256 "$out_dir/$name.zip" "$out_dir/$name.sha256"
done

(
  cd "$out_dir"
  cat ./*.sha256 | sort -k2 > checksums.txt
)

rm -rf "$staging_dir"
if [[ "${GOCACHE:-}" == "" ]]; then
  rm -rf "$cache_dir"
fi

echo "release artifacts:"
find "$out_dir" -maxdepth 1 -type f -print | sort
