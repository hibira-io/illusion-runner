// Package config stores the runner's settings and its token on this machine.
//
// Everything lives in one folder (Dir): config.json for settings, token for
// the runner token (file mode 0600). Moving the token to the OS keychain is
// planned; the file is only readable by the user who paired the runner.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// DefaultServer is the Illusion site the runner talks to.
const DefaultServer = "https://illusion.hibira.io"

// SDCpp configures the stable-diffusion.cpp engine: the sd binary and model files.
type SDCpp struct {
	Bin string `json:"bin"`
	// Model files by role. Missing ones turn the matching tools off.
	SDXL            string `json:"sdxl,omitempty"`              // sd_xl_base_1.0.safetensors
	QwenImage       string `json:"qwen_image,omitempty"`        // Qwen-Image diffusion model (GGUF or safetensors)
	QwenImageEdit   string `json:"qwen_image_edit,omitempty"`   // Qwen-Image-Edit-2509 diffusion model
	QwenTextEncoder string `json:"qwen_text_encoder,omitempty"` // Qwen2.5-VL-7B
	QwenVAE         string `json:"qwen_vae,omitempty"`
	Upscaler        string `json:"upscaler,omitempty"` // RealESRGAN_x4plus.pth
	LoraDir         string `json:"lora_dir,omitempty"`
	// TextEncoderFlag is sd.cpp's flag for the Qwen text encoder ("--llm" in
	// current releases, "--qwen2vl" in older ones).
	TextEncoderFlag string   `json:"text_encoder_flag,omitempty"`
	ExtraArgs       []string `json:"extra_args,omitempty"` // e.g. ["--offload-to-cpu", "--diffusion-fa"]
}

// Comfy configures the ComfyUI engine: a ComfyUI listening on this machine only.
type Comfy struct {
	URL string `json:"url"` // http://127.0.0.1:8188
}

// Config is config.json.
type Config struct {
	Server   string `json:"server"`
	RunnerID string `json:"runner_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Engine   string `json:"engine"` // "comfy" or "sdcpp"
	Comfy    Comfy  `json:"comfy"`
	SDCpp    SDCpp  `json:"sdcpp"`
	// GPUs to use (indexes); empty = all.
	GPUs []int `json:"gpus,omitempty"`
}

// Dir is where the runner keeps its files: $ILLUSION_RUNNER_HOME, else the
// user's config folder (e.g. ~/Library/Application Support/IllusionRunner,
// %AppData%\IllusionRunner, ~/.config/IllusionRunner).
func Dir() (string, error) {
	if d := os.Getenv("ILLUSION_RUNNER_HOME"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "IllusionRunner"), nil
}

// Defaults for a new install.
func Defaults() Config {
	return Config{Server: DefaultServer, Engine: "comfy", Comfy: Comfy{URL: "http://127.0.0.1:8188"},
		SDCpp: SDCpp{Bin: "sd-cli", TextEncoderFlag: "--llm"}}
}

// Load reads config.json in dir, or the defaults when there is none.
func Load(dir string) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	c.Server = strings.TrimRight(c.Server, "/")
	return c, nil
}

// Save writes config.json in dir.
func Save(dir string, c Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "config.json"), append(b, '\n'), 0o600)
}

// LoadToken reads the runner token, "" when the runner isn't paired.
func LoadToken(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "token"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return strings.TrimSpace(string(b)), err
}

// SaveToken stores the runner token, readable by this user only.
func SaveToken(dir, token string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "token"), []byte(token+"\n"), 0o600)
}

// ForgetToken removes the token (unpairing this machine).
func ForgetToken(dir string) error {
	err := os.Remove(filepath.Join(dir, "token"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
