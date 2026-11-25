// Package engine runs a job's ops on this machine's GPU.
//
// An engine takes one op at a time (op protocol 1): t2i, edit, upscale_x4,
// cutout. Files are passed by name: the job's inputs (in0, in1, ...) and
// earlier ops' outputs, all in the job's work folder.
package engine

import (
	"context"

	"github.com/hibira-io/illusion-runner/internal/api"
)

// Op names a runner reports ready (genai-studio site/runner_plan.py NEEDS).
const (
	T2ISDXL       = "t2i:sdxl"
	T2IQwenImage  = "t2i:qwen_image"
	EditQwenImage = "edit:qwen_image_edit"
	UpscaleX4     = "upscale_x4"
	Cutout        = "cutout"
)

// Key is how an op is named in ops_ready: "t2i:sdxl", "upscale_x4".
func Key(op api.Op) string {
	if op.Model != "" {
		return op.Op + ":" + op.Model
	}
	return op.Op
}

// Engine runs ops.
type Engine interface {
	// Name and Version say which engine this is (reported to the site).
	Name() string
	Version(ctx context.Context) string
	// Ready lists the ops this engine can run now (its models are present).
	Ready(ctx context.Context) []string
	// Run runs one op on the given GPU: inputs and outputs are files in dir,
	// named as in the op (the output is dir/op.Out, a PNG).
	Run(ctx context.Context, gpu int, op api.Op, dir string) error
}
