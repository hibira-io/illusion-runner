#!/usr/bin/env bash
# Stage the built stable-diffusion.cpp binaries into a release folder and zip it.
#
# Usage: scripts/package.sh <build-bin-dir> <sd-source-dir> <package-name> <backend>
# Produces dist/<package-name>.zip containing a single <package-name>/ folder.
set -euo pipefail

bin_dir=$1
sd_dir=$2
name=$3
backend=$4

root=$(cd "$(dirname "$0")/.." && pwd)
stage="$root/stage/$name"
rm -rf "$stage"
mkdir -p "$stage/licenses" "$root/dist"

# Executables and shared libraries (cp -a keeps .so symlinks intact).
cp -a "$bin_dir"/. "$stage"/

cp "$sd_dir/start-server.sh" "$sd_dir/start-server.bat" "$stage"/
chmod +x "$stage/start-server.sh"

cp "$sd_dir/LICENSE" "$stage/licenses/stable-diffusion.cpp.txt"
cp "$sd_dir/ggml/LICENSE" "$stage/licenses/ggml.txt"
cp "$sd_dir/thirdparty/libwebp/COPYING" "$stage/licenses/libwebp.txt"
cp "$sd_dir/thirdparty/libwebm/LICENSE.TXT" "$stage/licenses/libwebm.txt"
cp "$root/LICENSE" "$stage/licenses/illusion-runner.txt"
cp "$root/README.md" "$stage/README.md"

sd_sha=$(git -C "$sd_dir" rev-parse HEAD)
cat > "$stage/VERSION.txt" <<EOF
illusion-runner ${RUNNER_VERSION:-dev}
backend ${backend}
stable-diffusion.cpp ${sd_sha}
built $(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF

# Smoke test from the staged folder, not the build tree, so missing
# libraries or absolute rpaths fail here instead of on a customer's machine.
(cd "$stage" && ./sd-cli --version)

(cd "$root/stage" && zip -qry "$root/dist/$name.zip" "$name")
echo "Packaged dist/$name.zip"
