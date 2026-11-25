// Command illusion-runner runs your Illusion workspace's jobs on this
// machine's GPU.
//
//	illusion-runner pair      pair this machine with your workspace (shows a code)
//	illusion-runner run       wait for jobs and run them (Ctrl+C to stop)
//	illusion-runner status    show the settings, GPUs and the tools that are ready
//	illusion-runner unpair    forget this machine's token
//	illusion-runner version   print the version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/hibira-io/illusion-runner/internal/api"
	"github.com/hibira-io/illusion-runner/internal/config"
	"github.com/hibira-io/illusion-runner/internal/engine"
	"github.com/hibira-io/illusion-runner/internal/engine/comfy"
	"github.com/hibira-io/illusion-runner/internal/engine/sdcpp"
	"github.com/hibira-io/illusion-runner/internal/gpu"
	"github.com/hibira-io/illusion-runner/internal/runner"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `Illusion runner %s: run your Illusion jobs on this machine's GPU.

Usage:
  illusion-runner pair [-server URL] [-name NAME]   pair this machine with your workspace
  illusion-runner run                               wait for jobs and run them (Ctrl+C stops)
  illusion-runner status                            settings, GPUs and the tools that are ready
  illusion-runner unpair                            forget this machine's token
  illusion-runner version                           print the version

Settings are in %s (config.json).
`

func main() {
	os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr))
}

func cli(args []string, stdout, stderr io.Writer) int {
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, "no settings folder:", err)
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintf(stderr, usage, version, dir)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "version", "-version", "--version", "-v":
		fmt.Fprintf(stdout, "illusion-runner %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	case "pair":
		err = pair(ctx, dir, args[1:], stdout)
	case "run":
		err = run(ctx, dir, stdout, stderr)
	case "status":
		err = status(ctx, dir, stdout)
	case "unpair":
		if err = config.ForgetToken(dir); err == nil {
			fmt.Fprintln(stdout, "This machine is unpaired. Remove it in the portal too (Settings → My GPU).")
		}
	case "help", "-h", "--help":
		fmt.Fprintf(stdout, usage, version, dir)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n"+usage, args[0], version, dir)
		return 2
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// load reads the settings. Unpacked from an engine bundle (sd-cli beside the
// runner), the bundled stable-diffusion.cpp and its models folder are used
// unless config.json picks another engine.
func load(dir string) (config.Config, error) {
	c, err := config.Load(dir)
	if err != nil {
		return c, err
	}
	_, statErr := os.Stat(filepath.Join(dir, "config.json"))
	if exe, err := os.Executable(); err == nil && (statErr != nil || c.Engine == "sdcpp") {
		if exe, err = filepath.EvalSymlinks(exe); err == nil && config.Bundled(filepath.Dir(exe), &c.SDCpp) {
			c.Engine = "sdcpp"
		}
	}
	return c, nil
}

func hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "My computer"
	}
	return strings.TrimSuffix(h, ".local")
}

func pair(ctx context.Context, dir string, args []string, stdout io.Writer) error {
	c, err := load(dir)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	server := fs.String("server", c.Server, "Illusion site")
	name := fs.String("name", hostname(), "name shown in the portal")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c.Server = strings.TrimRight(*server, "/")
	cl := api.New(c.Server, "", version)
	p, err := cl.StartPairing(ctx, api.PairInfo{Name: *name, OS: runtime.GOOS + "/" + runtime.GOARCH, RunnerVersion: version})
	if err != nil {
		return fmt.Errorf("can't reach %s: %w", c.Server, err)
	}
	fmt.Fprintf(stdout, "\nIn the portal, open Settings → My GPU → Add a computer and enter this code:\n\n    %s\n\n", p.Code)
	fmt.Fprintf(stdout, "Waiting for you to confirm (the code expires at %s)...\n", p.ExpiresAt)
	wait := time.Duration(max(p.PollAfterS, 2)) * time.Second
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		got, err := cl.Collect(ctx, p)
		if err != nil {
			var e *api.Error
			if errors.As(err, &e) && (e.Status == 404 || e.Status == 410) {
				return errors.New("the code expired; run `illusion-runner pair` again")
			}
			continue // a network blip: keep waiting until the code expires
		}
		if got.Status != "paired" || got.Token == "" {
			continue
		}
		if err := config.SaveToken(dir, got.Token); err != nil {
			return err
		}
		c.RunnerID, c.Name = got.RunnerID, got.Name
		if err := config.Save(dir, c); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Paired as %q. Start it with: illusion-runner run\n", got.Name)
		return nil
	}
}

func newEngine(c config.Config) (engine.Engine, error) {
	switch c.Engine {
	case "", "comfy", "comfyui":
		return comfy.New(c.Comfy.URL)
	case "sdcpp", "stable-diffusion.cpp":
		return sdcpp.New(c.SDCpp), nil
	}
	return nil, fmt.Errorf("unknown engine %q in config.json (use \"comfy\" or \"sdcpp\")", c.Engine)
}

func gpus(c config.Config) []api.GPU {
	all := gpu.Detect()
	if len(c.GPUs) == 0 {
		return all
	}
	want := map[int]bool{}
	for _, i := range c.GPUs {
		want[i] = true
	}
	var out []api.GPU
	for _, g := range all {
		if want[g.Index] {
			out = append(out, g)
		}
	}
	return out
}

func run(ctx context.Context, dir string, stdout, stderr io.Writer) error {
	c, err := load(dir)
	if err != nil {
		return err
	}
	token, err := config.LoadToken(dir)
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("this machine isn't paired yet: run `illusion-runner pair` first")
	}
	eng, err := newEngine(c)
	if err != nil {
		return err
	}
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return err
	}
	r := &runner.Runner{API: api.New(c.Server, token, version), Engine: eng, GPUs: gpus(c), Version: version,
		OS: runtime.GOOS + "/" + runtime.GOARCH, RAMMB: gpu.RAMMB(), WorkDir: work}
	fmt.Fprintf(stdout, "Illusion runner %s: %s, engine %s, %d GPU(s). Waiting for jobs; Ctrl+C stops.\n",
		version, c.Server, eng.Name(), len(r.GPUs))
	if ready := eng.Ready(ctx); len(ready) == 0 {
		fmt.Fprintln(stderr, "warning: no tool is ready yet (check the engine and model files with `illusion-runner status`)")
	}
	err = r.Run(ctx)
	if errors.Is(err, runner.ErrRevoked) {
		_ = config.ForgetToken(dir)
	}
	return err
}

func status(ctx context.Context, dir string, stdout io.Writer) error {
	c, err := load(dir)
	if err != nil {
		return err
	}
	token, _ := config.LoadToken(dir)
	paired := "no (run `illusion-runner pair`)"
	if token != "" {
		paired = "yes"
		if c.Name != "" {
			paired += ", as " + c.Name
		}
	}
	fmt.Fprintf(stdout, "Version:  %s (%s/%s)\nSettings: %s\nServer:   %s\nPaired:   %s\nEngine:   %s\n",
		version, runtime.GOOS, runtime.GOARCH, filepath.Join(dir, "config.json"), c.Server, paired, c.Engine)
	for _, g := range gpus(c) {
		fmt.Fprintf(stdout, "GPU %d:    %s, %d MB\n", g.Index, g.Name, g.VRAMMB)
	}
	eng, err := newEngine(c)
	if err != nil {
		return err
	}
	if v := eng.Version(ctx); v != "" {
		fmt.Fprintf(stdout, "Engine version: %s\n", v)
	}
	ready := eng.Ready(ctx)
	if len(ready) == 0 {
		fmt.Fprintln(stdout, "Ready:    nothing yet (is the engine running, are the model files in place?)")
	} else {
		fmt.Fprintf(stdout, "Ready:    %s\n", strings.Join(ready, ", "))
	}
	return nil
}
