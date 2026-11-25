// Package api is the client for the Illusion site's runner API
// (/api/runner/v1, genai-studio site/runner_api.py). The runner only ever
// calls out; nothing connects to it.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Prefix of every runner route.
const Prefix = "/api/runner/v1"

// Error is a refusal from the site: {"error": {"code", "message"}}.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s (%d): %s", e.Code, e.Status, e.Message)
	}
	return fmt.Sprintf("%s (%d)", e.Code, e.Status)
}

// Unauthorized reports whether the token was refused (revoked or unknown).
func Unauthorized(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusUnauthorized
}

// Client talks to one site with one token.
type Client struct {
	Base      string
	Token     string
	UserAgent string
	HTTP      *http.Client
}

// New returns a client for base (https://..., no trailing slash).
func New(base, token, version string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, UserAgent: "illusion-runner/" + version,
		HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, headers map[string]string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+Prefix+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error.Code == "" {
			e.Error.Code = http.StatusText(resp.StatusCode)
		}
		return &Error{Status: resp.StatusCode, Code: e.Error.Code, Message: e.Error.Message}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Pairing is the start of pairing: a code for the person, a secret for collecting.
type Pairing struct {
	PairingID  string `json:"pairing_id"`
	Secret     string `json:"secret"`
	Code       string `json:"code"`
	ExpiresAt  string `json:"expires_at"`
	PollAfterS int    `json:"poll_after_s"`
}

// PairInfo describes this machine when pairing starts.
type PairInfo struct {
	Name          string `json:"name"`
	OS            string `json:"os"`
	RunnerVersion string `json:"runner_version"`
}

// StartPairing asks the site for a code to show.
func (c *Client) StartPairing(ctx context.Context, info PairInfo) (Pairing, error) {
	var p Pairing
	err := c.do(ctx, http.MethodPost, "/pair", nil, info, &p)
	return p, err
}

// Collected is a pairing's state: "pending", or "paired" with the token (given once).
type Collected struct {
	Status     string `json:"status"`
	RunnerID   string `json:"runner_id"`
	Token      string `json:"token"`
	Name       string `json:"name"`
	PollAfterS int    `json:"poll_after_s"`
}

// Collect polls a pairing with its secret.
func (c *Client) Collect(ctx context.Context, p Pairing) (Collected, error) {
	var out Collected
	err := c.do(ctx, http.MethodGet, "/pair/"+url.PathEscape(p.PairingID), map[string]string{"X-Pairing-Secret": p.Secret}, nil, &out)
	return out, err
}

// GPU is one graphics card (or Apple Silicon's GPU with its unified memory).
type GPU struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	VRAMMB int    `json:"vram_mb"`
}

// Heartbeat is what the runner says about itself.
type Heartbeat struct {
	RunnerVersion string   `json:"runner_version"`
	Engine        string   `json:"engine"`
	EngineVersion string   `json:"engine_version"`
	OS            string   `json:"os"`
	RAMMB         int      `json:"ram_mb"`
	GPUs          []GPU    `json:"gpus"`
	OpsReady      []string `json:"ops_ready"`
	BusyGPUs      []int    `json:"busy_gpus"`
}

// HeartbeatReply tells the runner when to poll and whether it must update.
type HeartbeatReply struct {
	MinVersion string `json:"min_version"`
	Outdated   bool   `json:"outdated"`
	PollAfterS int    `json:"poll_after_s"`
}

// Heartbeat reports the runner's state.
func (c *Client) Heartbeat(ctx context.Context, h Heartbeat) (HeartbeatReply, error) {
	var out HeartbeatReply
	err := c.do(ctx, http.MethodPost, "/heartbeat", nil, h, &out)
	return out, err
}

// Input is a job input: a short-lived link to download it.
type Input struct {
	URL string `json:"url"`
}

// Op is one step of a job (op protocol 1, genai-studio site/runner_plan.py).
type Op struct {
	Op          string   `json:"op"`
	Model       string   `json:"model,omitempty"`
	Prompt      string   `json:"prompt,omitempty"`
	Negative    string   `json:"negative,omitempty"`
	Instruction string   `json:"instruction,omitempty"`
	Images      []string `json:"images,omitempty"`
	Image       string   `json:"image,omitempty"`
	Width       *int     `json:"width,omitempty"`
	Height      *int     `json:"height,omitempty"`
	Seed        int64    `json:"seed,omitempty"`
	Steps       int      `json:"steps,omitempty"`
	CFG         float64  `json:"cfg,omitempty"`
	Loras       []Lora   `json:"loras,omitempty"`
	Out         string   `json:"out"`
}

// Lora is a bundled LoRA with its strength.
type Lora struct {
	Name     string  `json:"name"`
	Strength float64 `json:"strength"`
}

// Plan is what a job asks for.
type Plan struct {
	Protocol int                       `json:"protocol"`
	Ops      []Op                      `json:"ops"`
	Outputs  map[string]map[string]any `json:"outputs"`
}

// Job is a leased job.
type Job struct {
	ID       string           `json:"id"`
	Pipeline string           `json:"pipeline"`
	GPU      int              `json:"gpu"`
	Plan     Plan             `json:"plan"`
	Inputs   map[string]Input `json:"inputs"`
}

// LeaseReply is the jobs leased to this runner (maybe none).
type LeaseReply struct {
	Jobs       []Job `json:"jobs"`
	PollAfterS int   `json:"poll_after_s"`
}

// Lease asks for up to one job per free GPU.
func (c *Client) Lease(ctx context.Context, freeGPUs []int) (LeaseReply, error) {
	var out LeaseReply
	err := c.do(ctx, http.MethodPost, "/lease", nil, map[string]any{"gpus": freeGPUs}, &out)
	return out, err
}

// Progress reports how a job is going; the reply says whether to stop.
func (c *Client) Progress(ctx context.Context, job, text string, percent int) (cancel bool, err error) {
	var out struct {
		Cancel bool `json:"cancel"`
	}
	err = c.do(ctx, http.MethodPost, "/jobs/"+url.PathEscape(job)+"/progress", nil,
		map[string]any{"progress": text, "percent": percent}, &out)
	return out.Cancel, err
}

// Upload is where to PUT one output file.
type Upload struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// Uploads asks for upload links for the job's declared outputs.
func (c *Client) Uploads(ctx context.Context, job string, names []string) (map[string]Upload, error) {
	var out struct {
		Uploads map[string]Upload `json:"uploads"`
	}
	err := c.do(ctx, http.MethodPost, "/jobs/"+url.PathEscape(job)+"/uploads", nil, map[string]any{"names": names}, &out)
	return out.Uploads, err
}

// Output is a finished file.
type Output struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// Complete ends a job with its outputs.
func (c *Client) Complete(ctx context.Context, job string, outputs []Output, gpuSeconds float64) error {
	return c.do(ctx, http.MethodPost, "/jobs/"+url.PathEscape(job)+"/complete", nil,
		map[string]any{"outputs": outputs, "gpu_seconds": gpuSeconds}, nil)
}

// Fail ends a job with an error; retryable gives it back to the queue.
func (c *Client) Fail(ctx context.Context, job, message string, retryable bool) error {
	if len(message) > 1000 {
		message = message[:1000]
	}
	return c.do(ctx, http.MethodPost, "/jobs/"+url.PathEscape(job)+"/fail", nil,
		map[string]any{"error": message, "retryable": retryable}, nil)
}

// Download fetches an input from its link (to the given writer, at most limit bytes).
func (c *Client) Download(ctx context.Context, link string, w io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", resp.Status)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err == nil && n > limit {
		err = errors.New("download: file too large")
	}
	return err
}

// Put uploads a file to an upload link.
func (c *Client) Put(ctx context.Context, u Upload, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.URL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	for k, v := range u.Headers {
		req.Header.Set(k, v)
	}
	req.ContentLength = int64(len(data))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upload: %s", resp.Status)
	}
	return nil
}
