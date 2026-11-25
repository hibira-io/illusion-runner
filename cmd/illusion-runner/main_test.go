package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionAndUnknownCommands(t *testing.T) {
	t.Setenv("ILLUSION_RUNNER_HOME", t.TempDir())
	var out, errb bytes.Buffer
	if code := cli([]string{"version"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "illusion-runner dev") {
		t.Fatalf("version: %d %q", code, out.String())
	}
	if code := cli([]string{"nope"}, &out, &errb); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
}

func TestRunNeedsPairing(t *testing.T) {
	t.Setenv("ILLUSION_RUNNER_HOME", t.TempDir())
	var out, errb bytes.Buffer
	if code := cli([]string{"run"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "isn't paired") {
		t.Fatalf("run unpaired: %d %q", code, errb.String())
	}
}
