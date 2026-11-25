package runner

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibira-io/illusion-runner/internal/api"
)

// fakeEngine writes "png:<op>" for each op and records what it ran.
type fakeEngine struct {
	mu  sync.Mutex
	ran []string
	err error
}

func (f *fakeEngine) Name() string                   { return "fake" }
func (f *fakeEngine) Version(context.Context) string { return "1" }
func (f *fakeEngine) Ready(context.Context) []string { return []string{"t2i:sdxl", "upscale_x4"} }
func (f *fakeEngine) Run(_ context.Context, gpu int, op api.Op, dir string) error {
	f.mu.Lock()
	f.ran = append(f.ran, op.Op+">"+op.Out)
	f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(filepath.Join(dir, op.Out), []byte("png:"+op.Out), 0o600)
}

// site fakes /api/runner/v1 with one job to hand out.
type site struct {
	mu        sync.Mutex
	srv       *httptest.Server
	leased    bool
	puts      map[string]string
	completed map[string]any
	failed    map[string]any
	heartbeat map[string]any
	revoked   bool
	done      chan struct{}
}

func newSite(t *testing.T) *site {
	s := &site{puts: map[string]string{}, done: make(chan struct{}, 1)}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if s.revoked || r.Header.Get("Authorization") != "Bearer ilr_test" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated"}}`))
			return false
		}
		return true
	}
	body := func(r *http.Request) map[string]any {
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		return m
	}
	mux.HandleFunc("/api/runner/v1/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			s.mu.Lock()
			s.heartbeat = body(r)
			s.mu.Unlock()
			_, _ = w.Write([]byte(`{"min_version":"0.1.0","poll_after_s":2}`))
		}
	})
	mux.HandleFunc("/api/runner/v1/lease", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.leased {
			_, _ = w.Write([]byte(`{"jobs":[],"poll_after_s":2}`))
			return
		}
		s.leased = true
		job := map[string]any{"id": "j1", "pipeline": "image.sdxl", "gpu": 0,
			"inputs": map[string]any{"in0": map[string]any{"url": s.srv.URL + "/input"}},
			"plan": map[string]any{"protocol": 1, "ops": []any{
				map[string]any{"op": "t2i", "model": "sdxl", "prompt": "x", "seed": 1, "steps": 4, "cfg": 6, "out": "00.png"},
				map[string]any{"op": "upscale_x4", "image": "00.png", "out": "00_x4.png"}},
				"outputs": map[string]any{"00.png": map[string]any{"seed": 1}, "00_x4.png": map[string]any{"seed": 1}}}}
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": []any{job}, "poll_after_s": 2})
	})
	mux.HandleFunc("/input", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("input")) })
	mux.HandleFunc("/api/runner/v1/jobs/j1/progress", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			_, _ = w.Write([]byte(`{"cancel":false}`))
		}
	})
	mux.HandleFunc("/api/runner/v1/jobs/j1/uploads", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		ups := map[string]any{}
		for _, n := range body(r)["names"].([]any) {
			ups[n.(string)] = map[string]any{"url": s.srv.URL + "/put/" + n.(string), "headers": map[string]string{"Content-Type": "image/png"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"uploads": ups})
	})
	mux.HandleFunc("/put/", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.puts[strings.TrimPrefix(r.URL.Path, "/put/")] = string(b)
		s.mu.Unlock()
	})
	mux.HandleFunc("/api/runner/v1/jobs/j1/complete", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			s.mu.Lock()
			s.completed = body(r)
			s.mu.Unlock()
			s.done <- struct{}{}
		}
	})
	mux.HandleFunc("/api/runner/v1/jobs/j1/fail", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			s.mu.Lock()
			s.failed = body(r)
			s.mu.Unlock()
			s.done <- struct{}{}
		}
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func newRunner(s *site, eng *fakeEngine, t *testing.T) *Runner {
	return &Runner{API: api.New(s.srv.URL, "ilr_test", "0.1.0"), Engine: eng, Version: "0.1.0", OS: "test",
		GPUs: []api.GPU{{Index: 0, Name: "Test GPU", VRAMMB: 24000}}, WorkDir: t.TempDir(),
		Log: log.New(io.Discard, "", 0)}
}

func runUntilDone(t *testing.T, s *site, r *Runner) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = r.Run(ctx) }()
	select {
	case <-s.done:
	case <-ctx.Done():
		t.Fatal("the job never ended")
	}
}

func TestAJobRunsItsOpsAndUploadsTheDeclaredOutputs(t *testing.T) {
	s := newSite(t)
	eng := &fakeEngine{}
	runUntilDone(t, s, newRunner(s, eng, t))
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.Join(eng.ran, ",") != "t2i>00.png,upscale_x4>00_x4.png" {
		t.Fatalf("ran %v", eng.ran)
	}
	if s.puts["00.png"] != "png:00.png" || s.puts["00_x4.png"] != "png:00_x4.png" {
		t.Fatalf("puts %v", s.puts)
	}
	outs := s.completed["outputs"].([]any)
	if len(outs) != 2 || outs[0].(map[string]any)["sha256"] == "" {
		t.Fatalf("completed %v", s.completed)
	}
	if s.heartbeat["ops_ready"] == nil || s.heartbeat["runner_version"] != "0.1.0" {
		t.Fatalf("heartbeat %v", s.heartbeat)
	}
}

func TestAnEngineErrorFailsTheJobWithoutRetry(t *testing.T) {
	s := newSite(t)
	eng := &fakeEngine{err: os.ErrPermission}
	runUntilDone(t, s, newRunner(s, eng, t))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil || s.failed["retryable"] != false || s.completed != nil {
		t.Fatalf("failed %v completed %v", s.failed, s.completed)
	}
}

func TestARevokedRunnerStops(t *testing.T) {
	s := newSite(t)
	s.revoked = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := newRunner(s, &fakeEngine{}, t).Run(ctx); err != ErrRevoked {
		t.Fatalf("err = %v", err)
	}
}
