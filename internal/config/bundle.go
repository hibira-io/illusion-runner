package config

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Bundled sets up the stable-diffusion.cpp engine when the runner was
// unpacked from an engine bundle: sd-cli sits next to the runner, and model
// files may sit in a "models" folder beside it. Fields already set are kept.
// It reports whether a bundled sd-cli was found.
func Bundled(exeDir string, s *SDCpp) bool {
	bin := filepath.Join(exeDir, "sd-cli")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if _, err := os.Stat(bin); err != nil {
		return false
	}
	if s.Bin == "" || s.Bin == "sd-cli" || s.Bin == "sd" {
		s.Bin = bin
	}
	FindModels(filepath.Join(exeDir, "models"), s)
	return true
}

// FindModels fills the empty model fields from files in dir, matched by their
// usual names (case-insensitive): the cloud's file names, or the common GGUF
// names such as qwen-image-Q4_K_M.gguf and Qwen2.5-VL-7B-Instruct-Q4_K_M.gguf.
func FindModels(dir string, s *SDCpp) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	pick := func(field *string, match func(n string) bool) {
		if *field != "" {
			return
		}
		for _, n := range names {
			if match(strings.ToLower(n)) {
				*field = filepath.Join(dir, n)
				return
			}
		}
	}
	model := func(n string) bool {
		return strings.HasSuffix(n, ".gguf") || strings.HasSuffix(n, ".safetensors")
	}
	qwenImage := func(n string) bool {
		return model(n) && (strings.HasPrefix(n, "qwen-image") || strings.HasPrefix(n, "qwen_image")) &&
			!strings.Contains(n, "vae")
	}
	pick(&s.SDXL, func(n string) bool { return model(n) && strings.HasPrefix(n, "sd_xl_base") })
	pick(&s.QwenImageEdit, func(n string) bool { return qwenImage(n) && strings.Contains(n, "edit") })
	pick(&s.QwenImage, func(n string) bool { return qwenImage(n) && !strings.Contains(n, "edit") })
	pick(&s.QwenVAE, func(n string) bool { return model(n) && strings.Contains(n, "qwen") && strings.Contains(n, "vae") })
	pick(&s.QwenTextEncoder, func(n string) bool {
		return model(n) && (strings.HasPrefix(n, "qwen2.5-vl") || strings.HasPrefix(n, "qwen_2.5_vl"))
	})
	pick(&s.Upscaler, func(n string) bool { return strings.HasPrefix(n, "realesrgan_x4plus") && strings.HasSuffix(n, ".pth") })
	if s.LoraDir == "" {
		if fi, err := os.Stat(filepath.Join(dir, "loras")); err == nil && fi.IsDir() {
			s.LoraDir = filepath.Join(dir, "loras")
		}
	}
}
