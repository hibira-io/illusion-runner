# Illusion Runner

Run your [Illusion](https://illusion.hibira.io) image jobs on your own GPU.
Pair this small program with your workspace, pick **Run on: My GPU** in the
portal, and the job runs on your machine at 0 credits. Results land in your
portal like any other: history, downloads, sharing and the API keep working.

The runner only makes outbound HTTPS requests to Illusion. It opens no port,
and Illusion never connects to your computer.

> Status: early preview. Needs a paid plan with the My GPU add-on.

## Downloads

Grab the zip for your machine from the [latest release](../../releases/latest).
The engine bundles carry the runner plus a ready-to-run
[stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp) engine (MIT):

| Platform | File | GPU support |
|---|---|---|
| macOS (Apple Silicon) | `illusion-runner-<ver>-macos-arm64-metal.zip` | Metal |
| Windows x64 | `illusion-runner-<ver>-windows-x64-vulkan.zip` | NVIDIA, AMD and Intel GPUs via Vulkan |
| Windows x64 | `illusion-runner-<ver>-windows-x64-cpu.zip` | CPU only |
| Linux x64 | `illusion-runner-<ver>-linux-x64-vulkan.zip` | NVIDIA, AMD and Intel GPUs via Vulkan |
| Linux x64 | `illusion-runner-<ver>-linux-x64-cpu.zip` | CPU only |

The runner alone (no engine), for ComfyUI or your own engine build, is
`illusion-runner-<ver>-<os>-<arch>.zip` for `linux`, `macos` and `windows` on
`x64` and `arm64`.

`SHA256SUMS.txt` in each release lists the checksums.

Each engine bundle contains `illusion-runner`, `sd-cli` (one-shot generation),
`sd-server` (HTTP server with a web UI), `start-server.sh` / `start-server.bat`,
`VERSION.txt` (runner version, backend and the exact stable-diffusion.cpp
commit), `NOTICE` and `licenses/`.

## Quick start

```bash
unzip illusion-runner-*-macos-arm64-metal.zip
cd illusion-runner-*-macos-arm64-metal
mkdir -p models   # put model files here (see Engines below)
./illusion-runner status
./illusion-runner pair
./illusion-runner run
```

From a bundle, the runner uses the `sd-cli` next to it and finds model files
in the `models` folder beside it by their usual names, so no settings are needed.

On macOS, builds that are not yet signed and notarized are quarantined by Gatekeeper
after download. Until signing is enabled, clear it with:

```bash
xattr -dr com.apple.quarantine illusion-runner-*-macos-arm64-metal
```

## Pair and run

```sh
illusion-runner pair     # shows a code; enter it in the portal: Settings → My GPU → Add a computer
illusion-runner run      # waits for jobs; Ctrl+C stops
illusion-runner status   # GPUs, engine, and which tools are ready
illusion-runner unpair   # forget this machine (remove it in the portal too)
```

While the runner is stopped, jobs sent to My GPU wait until it is back.
Every GPU in the machine is used, one job at a time each.

## Engines

The runner drives an image engine that you install separately. Set it in
`config.json` (see Settings below).

### ComfyUI (NVIDIA; the same results as the cloud)

Install [ComfyUI](https://github.com/comfyanonymous/ComfyUI) and start it on
this machine only (it listens on `127.0.0.1:8188` by default). Put the model
files in ComfyUI's `models` folders:

| Tool | File | Folder |
| --- | --- | --- |
| Image (SDXL) | `sd_xl_base_1.0.safetensors` | `checkpoints` |
| Image (Qwen-Image) | `qwen_image_2512_fp8_e4m3fn.safetensors` | `diffusion_models` |
| Edit (Qwen-Image-Edit) | `qwen_image_edit_2509_fp8_e4m3fn.safetensors` | `diffusion_models` |
| Qwen text encoder | `qwen_2.5_vl_7b_fp8_scaled.safetensors` | `text_encoders` |
| Qwen VAE | `qwen_image_vae.safetensors` | `vae` |
| Upscale ×4 | `RealESRGAN_x4plus.pth` | `upscale_models` |

Qwen-Image and Qwen-Image-Edit need about 24 GB of GPU memory; SDXL about 8 GB.
`illusion-runner status` lists the tools whose files it found.

```json
{ "engine": "comfy", "comfy": { "url": "http://127.0.0.1:8188" } }
```

### stable-diffusion.cpp (bundled; Mac, AMD, smaller GPUs; experimental)

[stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp) runs
without Python on Apple Silicon (Metal), NVIDIA (CUDA or Vulkan) and AMD
(Vulkan), and accepts smaller quantized (GGUF) models. Results can differ
slightly from the cloud. The engine bundles include it and need no settings:
put the model files in `models/` beside the runner (LoRAs in `models/loras/`).
To use your own `sd-cli` or model paths, set them in `config.json`; tools
whose files are missing are simply turned off.

```json
{
  "engine": "sdcpp",
  "sdcpp": {
    "bin": "/usr/local/bin/sd-cli",
    "sdxl": "/models/sd_xl_base_1.0.safetensors",
    "qwen_image": "/models/qwen-image-Q4_K_M.gguf",
    "qwen_image_edit": "/models/qwen-image-edit-2509-Q4_K_M.gguf",
    "qwen_text_encoder": "/models/Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf",
    "qwen_vae": "/models/qwen_image_vae.safetensors",
    "upscaler": "/models/RealESRGAN_x4plus.pth",
    "extra_args": ["--diffusion-fa"]
  }
}
```

Older stable-diffusion.cpp releases call the Qwen text encoder flag `--qwen2vl`; set
`"text_encoder_flag": "--qwen2vl"` for those.

## Settings

Settings live in one folder, `config.json` plus the runner's `token`
(readable by your user only):

* Windows: `%AppData%\IllusionRunner`
* macOS: `~/Library/Application Support/IllusionRunner`
* Linux: `~/.config/IllusionRunner`

Set `ILLUSION_RUNNER_HOME` to use another folder (for example, to run two
runners on one machine, each with its own GPUs in `"gpus": [0]`).

## What runs on your machine

Illusion sends each job as a short list of basic steps (generate, edit,
upscale ×4), with the prompt, seed and settings already chosen. The
runner downloads the job's input images, runs the steps with your engine,
uploads the declared outputs and reports back. Nothing else on your machine is
read or sent.

## Build from source

```sh
go test ./...
go build ./cmd/illusion-runner
scripts/build.sh v0.1.0   # the runner alone for every platform, into dist/
```

## Releasing

```bash
git tag v0.1.0
git push origin v0.1.0
```

Tags with a hyphen (for example `v0.2.0-rc.1`) are published as pre-releases.
Pushes to `main` and pull requests run the tests and the same builds and attach the zips as
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

## Licence and trademarks

Apache License 2.0, see [LICENSE](LICENSE). "Illusion" and "Hibira" are
trademarks of Hibira and are not licensed: see [NOTICE](NOTICE). ComfyUI,
stable-diffusion.cpp and the model weights are separate projects under their
own licences; bundled third-party licences are in each zip's `licenses/` folder.
