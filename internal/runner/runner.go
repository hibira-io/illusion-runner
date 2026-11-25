// Package runner is the job loop: report in, lease jobs for free GPUs, run
// their ops, upload the results, and say how it went.
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/hibira-io/illusion-runner/internal/api"
	"github.com/hibira-io/illusion-runner/internal/engine"
)

// ErrRevoked: the site refused the token (the runner was removed in the portal).
var ErrRevoked = errors.New("this runner was removed from its workspace; pair it again")

const (
	maxInput        = 100 << 20 // bytes per input file
	heartbeatEvery  = 30 * time.Second
	minPoll         = 2 * time.Second
	maxPoll         = 60 * time.Second
	maxOutputBytes  = 50 << 20
	protocolVersion = 1
)

// Runner runs jobs from one site on this machine's GPUs.
type Runner struct {
	API     *api.Client
	Engine  engine.Engine
	GPUs    []api.GPU
	Version string
	OS      string
	RAMMB   int
	WorkDir string
	Log     *log.Logger

	mu   sync.Mutex
	busy map[int]bool
	wake chan struct{}
}

func (r *Runner) gpuIndexes() []int {
	if len(r.GPUs) == 0 {
		return []int{0} // no GPU found: the engine decides (e.g. a CPU build)
	}
	var out []int
	for _, g := range r.GPUs {
		out = append(out, g.Index)
	}
	return out
}

func (r *Runner) free() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []int
	for _, i := range r.gpuIndexes() {
		if !r.busy[i] {
			out = append(out, i)
		}
	}
	return out
}

func (r *Runner) busyList() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []int{}
	for i, b := range r.busy {
		if b {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}

func (r *Runner) heartbeat(ctx context.Context) (api.HeartbeatReply, error) {
	return r.API.Heartbeat(ctx, api.Heartbeat{RunnerVersion: r.Version, Engine: r.Engine.Name(),
		EngineVersion: r.Engine.Version(ctx), OS: r.OS, RAMMB: r.RAMMB, GPUs: r.GPUs,
		OpsReady: r.Engine.Ready(ctx), BusyGPUs: r.busyList()})
}

func clampPoll(s int) time.Duration {
	d := time.Duration(s) * time.Second
	if d < minPoll {
		return minPoll
	}
	if d > maxPoll {
		return maxPoll
	}
	return d
}

// Run loops until ctx ends or the runner is revoked.
func (r *Runner) Run(ctx context.Context) error {
	r.busy, r.wake = map[int]bool{}, make(chan struct{}, 1)
	if r.Log == nil {
		r.Log = log.New(os.Stderr, "", log.LstdFlags)
	}
	hb, err := r.heartbeat(ctx)
	if api.Unauthorized(err) {
		return ErrRevoked
	}
	if err != nil {
		r.Log.Printf("can't reach Illusion yet: %v", err)
	} else if hb.Outdated {
		r.Log.Printf("this runner is out of date (Illusion needs %s or newer): download the latest release", hb.MinVersion)
	}
	poll := clampPoll(hb.PollAfterS)
	lastHB := time.Now()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		if time.Since(lastHB) >= heartbeatEvery {
			if hb, err = r.heartbeat(ctx); api.Unauthorized(err) {
				return ErrRevoked
			} else if err == nil {
				poll = clampPoll(hb.PollAfterS)
			}
			lastHB = time.Now()
		}
		if free := r.free(); len(free) > 0 {
			lease, err := r.API.Lease(ctx, free)
			switch {
			case api.Unauthorized(err):
				return ErrRevoked
			case err != nil:
				if ctx.Err() == nil {
					r.Log.Printf("lease: %v", err)
				}
			default:
				poll = clampPoll(lease.PollAfterS)
				for _, job := range lease.Jobs {
					job := job
					r.mu.Lock()
					r.busy[job.GPU] = true
					r.mu.Unlock()
					wg.Add(1)
					go func() {
						defer wg.Done()
						r.do(ctx, job)
						r.mu.Lock()
						delete(r.busy, job.GPU)
						r.mu.Unlock()
						select {
						case r.wake <- struct{}{}:
						default:
						}
					}()
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.wake:
		case <-time.After(poll):
		}
	}
}

// transient errors give the job back to the queue; the rest end it.
type transient struct{ error }

func (r *Runner) do(ctx context.Context, job api.Job) {
	start := time.Now()
	r.Log.Printf("job %s (%s) on GPU %d", job.ID, job.Pipeline, job.GPU)
	err := r.run(ctx, job, start)
	if err == nil {
		r.Log.Printf("job %s done in %s", job.ID, time.Since(start).Round(time.Second))
		return
	}
	if errors.Is(err, context.Canceled) && ctx.Err() == nil {
		r.Log.Printf("job %s cancelled", job.ID)
		return
	}
	var t transient
	retry := errors.As(err, &t) || ctx.Err() != nil
	r.Log.Printf("job %s failed: %v", job.ID, err)
	fctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if ferr := r.API.Fail(fctx, job.ID, err.Error(), retry); ferr != nil {
		r.Log.Printf("job %s: couldn't report the failure: %v", job.ID, ferr)
	}
}

func (r *Runner) run(ctx context.Context, job api.Job, start time.Time) error {
	if job.Plan.Protocol != protocolVersion {
		return fmt.Errorf("this runner speaks op protocol %d, the job needs %d: update the runner", protocolVersion, job.Plan.Protocol)
	}
	dir, err := os.MkdirTemp(r.WorkDir, "job-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, in := range job.Inputs {
		if filepath.Base(name) != name {
			return fmt.Errorf("bad input name %q", name)
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		err = r.API.Download(ctx, in.URL, f, maxInput)
		f.Close()
		if err != nil {
			return transient{err}
		}
	}
	jctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i, op := range job.Plan.Ops {
		if filepath.Base(op.Out) != op.Out {
			return fmt.Errorf("bad output name %q", op.Out)
		}
		stop, err := r.API.Progress(ctx, job.ID, fmt.Sprintf("%s %d/%d", op.Op, i+1, len(job.Plan.Ops)), i*100/len(job.Plan.Ops))
		if err != nil {
			return transient{err}
		}
		if stop {
			return context.Canceled
		}
		if err := r.Engine.Run(jctx, job.GPU, op, dir); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(job.Plan.Outputs))
	for n := range job.Plan.Outputs {
		names = append(names, n)
	}
	sort.Strings(names)
	ups, err := r.API.Uploads(ctx, job.ID, names)
	if err != nil {
		return transient{err}
	}
	var outs []api.Output
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return fmt.Errorf("output %s missing: %w", n, err)
		}
		if len(data) > maxOutputBytes {
			return fmt.Errorf("output %s is too large", n)
		}
		u, ok := ups[n]
		if !ok {
			return fmt.Errorf("no upload link for %s", n)
		}
		if err := r.API.Put(ctx, u, data); err != nil {
			return transient{err}
		}
		sum := sha256.Sum256(data)
		outs = append(outs, api.Output{Name: n, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))})
	}
	if err := r.API.Complete(ctx, job.ID, outs, time.Since(start).Seconds()); err != nil {
		return transient{err}
	}
	return nil
}
