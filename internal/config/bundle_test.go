package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestABundleFindsItsEngineAndModels(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "sd-cli")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	touch(t, bin)
	m := filepath.Join(dir, "models")
	for _, n := range []string{"sd_xl_base_1.0.safetensors", "qwen-image-Q4_K_M.gguf", "Qwen-Image-Edit-2509-Q4_K_M.gguf",
		"Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf", "qwen_image_vae.safetensors", "RealESRGAN_x4plus.pth", "loras/a.safetensors"} {
		touch(t, filepath.Join(m, n))
	}
	s := Defaults().SDCpp
	if !Bundled(dir, &s) {
		t.Fatal("bundle not found")
	}
	want := map[string]string{"bin": bin, "sdxl": "sd_xl_base_1.0.safetensors", "t2i": "qwen-image-Q4_K_M.gguf",
		"edit": "Qwen-Image-Edit-2509-Q4_K_M.gguf", "te": "Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf",
		"vae": "qwen_image_vae.safetensors", "up": "RealESRGAN_x4plus.pth", "loras": "loras"}
	got := map[string]string{"bin": s.Bin, "sdxl": filepath.Base(s.SDXL), "t2i": filepath.Base(s.QwenImage),
		"edit": filepath.Base(s.QwenImageEdit), "te": filepath.Base(s.QwenTextEncoder), "vae": filepath.Base(s.QwenVAE),
		"up": filepath.Base(s.Upscaler), "loras": filepath.Base(s.LoraDir)}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestNoBundleWithoutSdCli(t *testing.T) {
	s := SDCpp{SDXL: "/mine.safetensors"}
	if Bundled(t.TempDir(), &s) || s.SDXL != "/mine.safetensors" {
		t.Fatalf("got %+v", s)
	}
}
