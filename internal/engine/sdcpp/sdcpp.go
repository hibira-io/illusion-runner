// Package sdcpp runs ops with stable-diffusion.cpp (MIT,
// github.com/leejet/stable-diffusion.cpp): one `sd` process per op, no
// Python. It runs on NVIDIA (CUDA/Vulkan builds), AMD (Vulkan) and Apple
// Silicon (Metal), with GGUF quantized models for smaller GPUs.
//
// Experimental: results differ slightly from the cloud's ComfyUI (another
// engine, often smaller model files), and flags change between sd.cpp
// releases (config.SDCpp.TextEncoderFlag, ExtraArgs). The engine trial
// (docs/engine-trial.md) decides which tools it is offered for.
package sdcpp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hibira-io/illusion-runner/internal/api"
	"github.com/hibira-io/illusion-runner/internal/config"
	"github.com/hibira-io/illusion-runner/internal/engine"
)

// Engine is an sd binary and its model files.
type Engine struct {
	C config.SDCpp
	// exec is replaced in tests.
	exec func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
}

// New returns the engine for this configuration.
func New(c config.SDCpp) *Engine {
	if c.TextEncoderFlag == "" {
		c.TextEncoderFlag = "--llm"
	}
	return &Engine{C: c, exec: run}
}

func run(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.Bytes(), err
}

func (e *Engine) Name() string { return "stable-diffusion.cpp" }

func (e *Engine) Version(ctx context.Context) string {
	out, err := e.exec(ctx, nil, e.C.Bin, "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

func exists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// Ready: the ops whose files are configured and present.
func (e *Engine) Ready(ctx context.Context) []string {
	if _, err := exec.LookPath(e.C.Bin); err != nil && !exists(e.C.Bin) {
		return nil
	}
	var ready []string
	if exists(e.C.SDXL) {
		ready = append(ready, engine.T2ISDXL)
	}
	qwen := exists(e.C.QwenTextEncoder) && exists(e.C.QwenVAE)
	if qwen && exists(e.C.QwenImage) {
		ready = append(ready, engine.T2IQwenImage)
	}
	if qwen && exists(e.C.QwenImageEdit) {
		ready = append(ready, engine.EditQwenImage)
	}
	if exists(e.C.Upscaler) {
		ready = append(ready, engine.UpscaleX4)
	}
	return ready
}

// prompt adds the op's LoRAs in sd.cpp's prompt syntax: <lora:name:strength>.
func prompt(text string, loras []api.Lora) string {
	var b strings.Builder
	b.WriteString(text)
	for _, l := range loras {
		fmt.Fprintf(&b, " <lora:%s:%s>", strings.TrimSuffix(l.Name, filepath.Ext(l.Name)),
			strconv.FormatFloat(l.Strength, 'f', -1, 64))
	}
	return b.String()
}

func size(p *int, d int) string {
	if p == nil || *p <= 0 {
		return strconv.Itoa(d)
	}
	return strconv.Itoa(*p)
}

// Args is the sd command line for an op (exported for tests and the trial).
func (e *Engine) Args(op api.Op, dir string) ([]string, error) {
	outPath := filepath.Join(dir, op.Out)
	common := func(args ...string) []string {
		args = append(args, "-s", strconv.FormatInt(op.Seed, 10), "--steps", strconv.Itoa(op.Steps),
			"--cfg-scale", strconv.FormatFloat(op.CFG, 'f', -1, 64), "-o", outPath)
		if len(op.Loras) > 0 && e.C.LoraDir != "" {
			args = append(args, "--lora-model-dir", e.C.LoraDir)
		}
		return append(args, e.C.ExtraArgs...)
	}
	switch engine.Key(op) {
	case engine.T2ISDXL:
		return common("-m", e.C.SDXL, "-p", prompt(op.Prompt, op.Loras), "-n", op.Negative,
			"-W", size(op.Width, 1024), "-H", size(op.Height, 1024),
			"--sampling-method", "dpm++2m", "--scheduler", "karras"), nil
	case engine.T2IQwenImage:
		return common("--diffusion-model", e.C.QwenImage, "--vae", e.C.QwenVAE, e.C.TextEncoderFlag, e.C.QwenTextEncoder,
			"-p", prompt(op.Prompt, op.Loras), "-n", op.Negative, "-W", size(op.Width, 1328), "-H", size(op.Height, 1328),
			"--sampling-method", "euler", "--scheduler", "simple", "--flow-shift", "3.1"), nil
	case engine.EditQwenImage:
		args := []string{"--diffusion-model", e.C.QwenImageEdit, "--vae", e.C.QwenVAE, e.C.TextEncoderFlag, e.C.QwenTextEncoder,
			"-p", prompt(op.Instruction, op.Loras), "--sampling-method", "euler", "--scheduler", "simple", "--flow-shift", "3"}
		for _, img := range op.Images {
			args = append(args, "-r", filepath.Join(dir, img))
		}
		if op.Width != nil && op.Height != nil {
			args = append(args, "-W", size(op.Width, 1024), "-H", size(op.Height, 1024))
		}
		return common(args...), nil
	case engine.UpscaleX4:
		return append([]string{"-M", "upscale", "-i", filepath.Join(dir, op.Image), "--upscale-model", e.C.Upscaler,
			"-o", outPath}, e.C.ExtraArgs...), nil
	}
	return nil, fmt.Errorf("stable-diffusion.cpp can't run %s", engine.Key(op))
}

// Run runs sd for one op on the given GPU.
func (e *Engine) Run(ctx context.Context, gpu int, op api.Op, dir string) error {
	args, err := e.Args(op, dir)
	if err != nil {
		return err
	}
	env := []string{"CUDA_VISIBLE_DEVICES=" + strconv.Itoa(gpu), "GGML_VK_VISIBLE_DEVICES=" + strconv.Itoa(gpu)}
	out, err := e.exec(ctx, env, e.C.Bin, args...)
	if err != nil {
		tail := string(out)
		if len(tail) > 800 {
			tail = tail[len(tail)-800:]
		}
		return fmt.Errorf("stable-diffusion.cpp: %v: %s", err, strings.TrimSpace(tail))
	}
	if !exists(filepath.Join(dir, op.Out)) {
		return fmt.Errorf("stable-diffusion.cpp wrote no image")
	}
	return nil
}
