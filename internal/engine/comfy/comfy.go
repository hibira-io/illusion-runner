// Package comfy runs ops on a ComfyUI listening on this machine
// (127.0.0.1 only), at the version genai-studio pins and validates its graphs
// against. ComfyUI is GPL-3.0 and installed separately; the runner only talks
// to it over its HTTP API.
package comfy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hibira-io/illusion-runner/internal/api"
	"github.com/hibira-io/illusion-runner/internal/engine"
)

// Engine is a ComfyUI at URL.
type Engine struct {
	URL  string
	HTTP *http.Client
	Poll time.Duration
}

// New returns the engine for a local ComfyUI.
func New(u string) (*Engine, error) {
	p, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	host := p.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, fmt.Errorf("ComfyUI must listen on this machine only (127.0.0.1), not %s", host)
	}
	return &Engine{URL: strings.TrimRight(u, "/"), HTTP: &http.Client{Timeout: 60 * time.Second}, Poll: time.Second}, nil
}

func (e *Engine) Name() string { return "comfyui" }

func (e *Engine) Version(ctx context.Context) string {
	var s struct {
		System struct {
			ComfyUIVersion string `json:"comfyui_version"`
		} `json:"system"`
	}
	if err := e.getJSON(ctx, "/system_stats", &s); err != nil {
		return ""
	}
	return s.System.ComfyUIVersion
}

func (e *Engine) models(ctx context.Context, folder string) map[string]bool {
	var names []string
	_ = e.getJSON(ctx, "/models/"+folder, &names)
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// Ready: the ops whose model files ComfyUI has.
func (e *Engine) Ready(ctx context.Context) []string {
	ckpt, unet := e.models(ctx, "checkpoints"), e.models(ctx, "diffusion_models")
	te, vae, up := e.models(ctx, "text_encoders"), e.models(ctx, "vae"), e.models(ctx, "upscale_models")
	var ready []string
	if ckpt[SDXLCkpt] {
		ready = append(ready, engine.T2ISDXL)
	}
	if unet[QwenT2IUNet] && te[QwenTE] && vae[QwenVAE] {
		ready = append(ready, engine.T2IQwenImage)
	}
	if unet[QwenEditUNet] && te[QwenTE] && vae[QwenVAE] {
		ready = append(ready, engine.EditQwenImage)
	}
	if up[Upscaler] {
		ready = append(ready, engine.UpscaleX4)
	}
	return ready
}

// Run builds the op's graph, uploads its input images, queues it and saves the result.
func (e *Engine) Run(ctx context.Context, gpu int, op api.Op, dir string) error {
	prefix := "illusion-runner/" + strings.TrimSuffix(op.Out, ".png")
	var g Graph
	switch engine.Key(op) {
	case engine.T2ISDXL:
		g = SDXLT2I(op, prefix)
	case engine.T2IQwenImage:
		g = QwenT2I(op, prefix)
	case engine.EditQwenImage:
		var names []string
		for _, f := range op.Images {
			n, err := e.upload(ctx, filepath.Join(dir, f))
			if err != nil {
				return err
			}
			names = append(names, n)
		}
		var err error
		if g, err = QwenEdit(op, names, prefix); err != nil {
			return err
		}
	case engine.UpscaleX4:
		n, err := e.upload(ctx, filepath.Join(dir, op.Image))
		if err != nil {
			return err
		}
		g = UpscaleX4(n, prefix)
	default:
		return fmt.Errorf("ComfyUI can't run %s", engine.Key(op))
	}
	id, err := e.queue(ctx, g)
	if err != nil {
		return err
	}
	img, err := e.wait(ctx, id)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, op.Out), img, 0o600)
}

func (e *Engine) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL+path, nil)
	if err != nil {
		return err
	}
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ComfyUI %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Engine) upload(ctx context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("image", "illusion-"+filepath.Base(path)+".png")
	if err != nil {
		return "", err
	}
	_, _ = fw.Write(data)
	_ = w.WriteField("overwrite", "true")
	_ = w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL+"/upload/image", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ComfyUI upload: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Subfolder != "" {
		return out.Subfolder + "/" + out.Name, nil
	}
	return out.Name, nil
}

func (e *Engine) queue(ctx context.Context, g Graph) (string, error) {
	b, _ := json.Marshal(map[string]any{"prompt": g, "client_id": "illusion-runner"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL+"/prompt", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ComfyUI refused the graph: %s %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var out struct {
		PromptID string `json:"prompt_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.PromptID == "" {
		return "", errors.New("ComfyUI gave no prompt id")
	}
	return out.PromptID, nil
}

func (e *Engine) wait(ctx context.Context, id string) ([]byte, error) {
	for {
		var hist map[string]struct {
			Status struct {
				StatusStr string `json:"status_str"`
				Completed bool   `json:"completed"`
			} `json:"status"`
			Outputs map[string]struct {
				Images []struct {
					Filename  string `json:"filename"`
					Subfolder string `json:"subfolder"`
					Type      string `json:"type"`
				} `json:"images"`
			} `json:"outputs"`
		}
		if err := e.getJSON(ctx, "/history/"+url.PathEscape(id), &hist); err != nil {
			return nil, err
		}
		if h, ok := hist[id]; ok {
			if h.Status.StatusStr == "error" {
				return nil, errors.New("ComfyUI failed to run the graph")
			}
			for _, o := range h.Outputs {
				for _, im := range o.Images {
					q := url.Values{"filename": {im.Filename}, "subfolder": {im.Subfolder}, "type": {im.Type}}
					return e.fetch(ctx, "/view?"+q.Encode())
				}
			}
			if h.Status.Completed {
				return nil, errors.New("ComfyUI finished without an image")
			}
		}
		select {
		case <-ctx.Done():
			e.interrupt()
			return nil, ctx.Err()
		case <-time.After(e.Poll):
		}
	}
}

func (e *Engine) fetch(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ComfyUI view: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

func (e *Engine) interrupt() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, e.URL+"/interrupt", nil)
	if resp, err := e.HTTP.Do(req); err == nil {
		resp.Body.Close()
	}
}
