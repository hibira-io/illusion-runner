# Illusion Runner

Generate images on your own GPU with Hibira Illusion.

The runner ships the [stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp)
engine (MIT) as ready-to-run binaries for each platform. Every tag `v*` builds all
platforms on GitHub Actions and publishes them as a GitHub Release.

## Downloads

Grab the zip for your machine from the [latest release](../../releases/latest):

| Platform | File | GPU support |
|---|---|---|
| macOS (Apple Silicon) | `illusion-runner-<ver>-macos-arm64-metal.zip` | Metal |
| Windows x64 | `illusion-runner-<ver>-windows-x64-vulkan.zip` | NVIDIA, AMD and Intel GPUs via Vulkan |
| Windows x64 | `illusion-runner-<ver>-windows-x64-cpu.zip` | CPU only |
| Linux x64 | `illusion-runner-<ver>-linux-x64-vulkan.zip` | NVIDIA, AMD and Intel GPUs via Vulkan |
| Linux x64 | `illusion-runner-<ver>-linux-x64-cpu.zip` | CPU only |

`SHA256SUMS.txt` in each release lists the checksums.

Each zip contains `sd-cli` (one-shot generation), `sd-server` (HTTP server with a
web UI), `start-server.sh` / `start-server.bat`, `VERSION.txt` (runner version,
backend and the exact stable-diffusion.cpp commit) and `licenses/`.

## Quick start

```bash
unzip illusion-runner-*-macos-arm64-metal.zip
cd illusion-runner-*-macos-arm64-metal
./sd-cli --version
mkdir -p models   # put model files here
./start-server.sh
```

On macOS, builds that are not yet signed and notarized are quarantined by Gatekeeper
after download. Until signing is enabled, clear it with:

```bash
xattr -dr com.apple.quarantine illusion-runner-*-macos-arm64-metal
```

## Releasing

```bash
git tag v0.1.0
git push origin v0.1.0
```

Tags with a hyphen (for example `v0.2.0-rc.1`) are published as pre-releases.
Pushes to `main` and pull requests run the same builds and attach the zips as
workflow artifacts (Actions tab → run → Artifacts) without creating a release.

## Updating the engine

The stable-diffusion.cpp version is pinned by commit in
`.github/workflows/build.yml` (`SD_CPP_REF`). Change it in a pull request and the
CI builds will confirm every platform still compiles and starts.

## macOS signing and notarization

The macOS job signs and notarizes automatically once these repository secrets exist
(Settings → Secrets and variables → Actions):

| Secret | Value |
|---|---|
| `APPLE_CERT_P12_BASE64` | Developer ID Application certificate + key exported as .p12, base64-encoded |
| `APPLE_CERT_PASSWORD` | Password of that .p12 |
| `APPLE_SIGNING_IDENTITY` | e.g. `Developer ID Application: Your Company (TEAMID)` |
| `APPLE_ID` | Apple ID email used for notarization |
| `APPLE_APP_PASSWORD` | App-specific password for that Apple ID |
| `APPLE_TEAM_ID` | 10-character team ID |

Without them the macOS zip is built unsigned. Windows builds are unsigned.

## Not yet included

- Windows CUDA build (Vulkan covers NVIDIA for now; CUDA compiles take 1h+ in CI).
- AMD ROCm builds.
- Linux arm64.

## Licence

MIT, see [LICENSE](LICENSE). Bundled third-party licences are in each zip's `licenses/` folder.
