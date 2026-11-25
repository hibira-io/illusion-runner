package sdcpp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hibira-io/illusion-runner/internal/api"
	"github.com/hibira-io/illusion-runner/internal/config"
)

func ip(v int) *int { return &v }

func TestArgsCarryTheOpsSettings(t *testing.T) {
	e := New(config.SDCpp{Bin: "sd", SDXL: "/m/sdxl.safetensors", QwenImage: "/m/q.gguf", QwenVAE: "/m/vae",
		QwenTextEncoder: "/m/te.gguf", LoraDir: "/m/loras", ExtraArgs: []string{"--diffusion-fa"}})
	args, err := e.Args(api.Op{Op: "t2i", Model: "qwen_image", Prompt: "a mug", Negative: "ugly", Width: ip(1024),
		Height: ip(768), Seed: 42, Steps: 8, CFG: 1, Loras: []api.Lora{{Name: "light.safetensors", Strength: 0.8}},
		Out: "00.png"}, "/w")
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"--diffusion-model /m/q.gguf", "--llm /m/te.gguf", "-p a mug <lora:light:0.8>",
		"-W 1024 -H 768", "-s 42", "--steps 8", "--cfg-scale 1", "--flow-shift 3.1", "--lora-model-dir /m/loras",
		"-o " + filepath.Join("/w", "00.png"), "--diffusion-fa"} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in %q", want, line)
		}
	}
}

func TestEditPassesEachReferenceImage(t *testing.T) {
	e := New(config.SDCpp{Bin: "sd", QwenImageEdit: "/m/e.gguf", QwenVAE: "/m/vae", QwenTextEncoder: "/m/te"})
	args, err := e.Args(api.Op{Op: "edit", Model: "qwen_image_edit", Images: []string{"in0", "in1"}, Instruction: "blue",
		Seed: 1, Steps: 20, CFG: 2.5, Out: "00.png"}, "/w")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Join(args, " "), " -r "); n != 2 {
		t.Fatalf("want 2 reference images, got %d: %v", n, args)
	}
}

func TestReadyNeedsTheFiles(t *testing.T) {
	dir := t.TempDir()
	sdxl := filepath.Join(dir, "sdxl.safetensors")
	_ = os.WriteFile(sdxl, []byte("x"), 0o600)
	bin := filepath.Join(dir, "sd")
	_ = os.WriteFile(bin, []byte("x"), 0o700)
	e := New(config.SDCpp{Bin: bin, SDXL: sdxl, QwenImage: filepath.Join(dir, "missing.gguf")})
	got := e.Ready(context.Background())
	if len(got) != 1 || got[0] != "t2i:sdxl" {
		t.Fatalf("ready = %v", got)
	}
}

func TestRunReportsSdsError(t *testing.T) {
	e := New(config.SDCpp{Bin: "sd", SDXL: "/m/sdxl"})
	e.exec = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
		if !strings.Contains(strings.Join(env, " "), "CUDA_VISIBLE_DEVICES=1") {
			t.Errorf("GPU not selected: %v", env)
		}
		return []byte("out of memory"), os.ErrInvalid
	}
	err := e.Run(context.Background(), 1, api.Op{Op: "t2i", Model: "sdxl", Prompt: "x", Out: "00.png"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "out of memory") {
		t.Fatalf("err = %v", err)
	}
}
