package comfy

// The four op graphs, ported from genai-studio runtime/gateway/app/graphs.py
// (sdxl_t2i, qwen_t2i, qwen_edit, upscale): core ComfyUI nodes only, same
// models, samplers and settings, so a job looks the same on a customer's GPU
// as on the cloud node. testdata/graphs.json holds the Python builders' output
// for a set of inputs; graphs_test.go fails when the two drift apart.

import (
	"fmt"
	"math"
	"strconv"

	"github.com/hibira-io/illusion-runner/internal/api"
)

// Model files (genai-studio runtime/models.json).
const (
	QwenT2IUNet  = "qwen_image_2512_fp8_e4m3fn.safetensors"
	QwenEditUNet = "qwen_image_edit_2509_fp8_e4m3fn.safetensors"
	QwenTE       = "qwen_2.5_vl_7b_fp8_scaled.safetensors"
	QwenVAE      = "qwen_image_vae.safetensors"
	SDXLCkpt     = "sd_xl_base_1.0.safetensors"
	Upscaler     = "RealESRGAN_x4plus.pth"
)

// Graph is a ComfyUI API-format prompt.
type Graph map[string]Node

// Node is one node of a graph.
type Node struct {
	ClassType string         `json:"class_type"`
	Inputs    map[string]any `json:"inputs"`
}

type builder struct {
	g Graph
	n int
}

func newBuilder() *builder { return &builder{g: Graph{}} }

func (b *builder) add(class string, inputs map[string]any) string {
	b.n++
	id := strconv.Itoa(b.n)
	b.g[id] = Node{ClassType: class, Inputs: inputs}
	return id
}

func out(id string, i int) []any { return []any{id, i} }

func snap(v, m int) int {
	r := int(math.RoundToEven(float64(v)/float64(m))) * m // Python round(): halves to even
	if r < m {
		return m
	}
	return r
}

func loraOnly(b *builder, model []any, loras []api.Lora) []any {
	for _, l := range loras {
		id := b.add("LoraLoaderModelOnly", map[string]any{"model": model, "lora_name": l.Name, "strength_model": l.Strength})
		model = out(id, 0)
	}
	return model
}

func intOr(p *int, d int) int {
	if p == nil {
		return d
	}
	return *p
}

// SDXLT2I is graphs.sdxl_t2i.
func SDXLT2I(op api.Op, prefix string) Graph {
	b := newBuilder()
	ckpt := b.add("CheckpointLoaderSimple", map[string]any{"ckpt_name": SDXLCkpt})
	model, clip := out(ckpt, 0), out(ckpt, 1)
	for _, l := range op.Loras {
		id := b.add("LoraLoader", map[string]any{"model": model, "clip": clip, "lora_name": l.Name,
			"strength_model": l.Strength, "strength_clip": l.Strength})
		model, clip = out(id, 0), out(id, 1)
	}
	pos := b.add("CLIPTextEncode", map[string]any{"clip": clip, "text": op.Prompt})
	neg := b.add("CLIPTextEncode", map[string]any{"clip": clip, "text": op.Negative})
	lat := b.add("EmptyLatentImage", map[string]any{"width": snap(intOr(op.Width, 1024), 64),
		"height": snap(intOr(op.Height, 1024), 64), "batch_size": 1})
	ks := b.add("KSampler", map[string]any{"model": model, "positive": out(pos, 0), "negative": out(neg, 0),
		"latent_image": out(lat, 0), "seed": op.Seed, "steps": op.Steps, "cfg": op.CFG,
		"sampler_name": "dpmpp_2m", "scheduler": "karras", "denoise": 1.0})
	dec := b.add("VAEDecode", map[string]any{"samples": out(ks, 0), "vae": out(ckpt, 2)})
	b.add("SaveImage", map[string]any{"images": out(dec, 0), "filename_prefix": prefix})
	return b.g
}

// QwenT2I is graphs.qwen_t2i.
func QwenT2I(op api.Op, prefix string) Graph {
	b := newBuilder()
	unet := b.add("UNETLoader", map[string]any{"unet_name": QwenT2IUNet, "weight_dtype": "default"})
	clip := b.add("CLIPLoader", map[string]any{"clip_name": QwenTE, "type": "qwen_image", "device": "default"})
	vae := b.add("VAELoader", map[string]any{"vae_name": QwenVAE})
	model := loraOnly(b, out(unet, 0), op.Loras)
	ms := b.add("ModelSamplingAuraFlow", map[string]any{"model": model, "shift": 3.1})
	pos := b.add("CLIPTextEncode", map[string]any{"clip": out(clip, 0), "text": op.Prompt})
	neg := b.add("CLIPTextEncode", map[string]any{"clip": out(clip, 0), "text": op.Negative})
	lat := b.add("EmptySD3LatentImage", map[string]any{"width": snap(intOr(op.Width, 1328), 16),
		"height": snap(intOr(op.Height, 1328), 16), "batch_size": 1})
	ks := b.add("KSampler", map[string]any{"model": out(ms, 0), "positive": out(pos, 0), "negative": out(neg, 0),
		"latent_image": out(lat, 0), "seed": op.Seed, "steps": op.Steps, "cfg": op.CFG,
		"sampler_name": "euler", "scheduler": "simple", "denoise": 1.0})
	dec := b.add("VAEDecode", map[string]any{"samples": out(ks, 0), "vae": out(vae, 0)})
	b.add("SaveImage", map[string]any{"images": out(dec, 0), "filename_prefix": prefix})
	return b.g
}

// QwenEdit is graphs.qwen_edit; images are the names ComfyUI knows the inputs by.
func QwenEdit(op api.Op, images []string, prefix string) (Graph, error) {
	if len(images) < 1 || len(images) > 3 {
		return nil, fmt.Errorf("edit takes 1-3 reference images")
	}
	b := newBuilder()
	unet := b.add("UNETLoader", map[string]any{"unet_name": QwenEditUNet, "weight_dtype": "default"})
	clip := b.add("CLIPLoader", map[string]any{"clip_name": QwenTE, "type": "qwen_image", "device": "default"})
	vae := b.add("VAELoader", map[string]any{"vae_name": QwenVAE})
	model := loraOnly(b, out(unet, 0), op.Loras)
	ms := b.add("ModelSamplingAuraFlow", map[string]any{"model": model, "shift": 3.0})
	refs := map[string]any{}
	first := ""
	for i, name := range images {
		li := b.add("LoadImage", map[string]any{"image": name})
		sc := b.add("ImageScaleToTotalPixels", map[string]any{"image": out(li, 0), "upscale_method": "lanczos",
			"megapixels": 1.0, "resolution_steps": 16})
		refs["image"+strconv.Itoa(i+1)] = out(sc, 0)
		if first == "" {
			first = sc
		}
	}
	enc := func(text string) string {
		in := map[string]any{"clip": out(clip, 0), "prompt": text, "vae": out(vae, 0)}
		for k, v := range refs {
			in[k] = v
		}
		return b.add("TextEncodeQwenImageEditPlus", in)
	}
	pos := enc(op.Instruction)
	neg := enc("")
	var lat string
	if op.Width != nil && op.Height != nil && *op.Width > 0 && *op.Height > 0 {
		lat = b.add("EmptySD3LatentImage", map[string]any{"width": snap(*op.Width, 16), "height": snap(*op.Height, 16),
			"batch_size": 1})
	} else {
		lat = b.add("VAEEncode", map[string]any{"pixels": out(first, 0), "vae": out(vae, 0)})
	}
	ks := b.add("KSampler", map[string]any{"model": out(ms, 0), "positive": out(pos, 0), "negative": out(neg, 0),
		"latent_image": out(lat, 0), "seed": op.Seed, "steps": op.Steps, "cfg": op.CFG,
		"sampler_name": "euler", "scheduler": "simple", "denoise": 1.0})
	dec := b.add("VAEDecode", map[string]any{"samples": out(ks, 0), "vae": out(vae, 0)})
	b.add("SaveImage", map[string]any{"images": out(dec, 0), "filename_prefix": prefix})
	return b.g, nil
}

// UpscaleX4 is graphs.upscale.
func UpscaleX4(image, prefix string) Graph {
	b := newBuilder()
	um := b.add("UpscaleModelLoader", map[string]any{"model_name": Upscaler})
	img := b.add("LoadImage", map[string]any{"image": image})
	up := b.add("ImageUpscaleWithModel", map[string]any{"upscale_model": out(um, 0), "image": out(img, 0)})
	b.add("SaveImage", map[string]any{"images": out(up, 0), "filename_prefix": prefix})
	return b.g
}
