package comfy

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/hibira-io/illusion-runner/internal/api"
)

// testdata/graphs.json comes from genai-studio's graphs.py: the Go builders
// must produce the same graphs for the same ops.
func TestGraphsMatchTheGateway(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/graphs.json")
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Cases []struct {
			Kind  string          `json:"kind"`
			Op    api.Op          `json:"op"`
			Graph json.RawMessage `json:"graph"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Cases) == 0 {
		t.Fatal("no cases")
	}
	for i, c := range data.Cases {
		var g Graph
		switch c.Kind {
		case "sdxl":
			g = SDXLT2I(c.Op, "hibira/sdxl")
		case "qwen":
			g = QwenT2I(c.Op, "hibira/t2i")
		case "edit":
			g, err = QwenEdit(c.Op, c.Op.Images, "hibira/edit")
			if err != nil {
				t.Fatal(err)
			}
		case "upscale":
			g = UpscaleX4(c.Op.Image, "hibira/upscale")
		}
		var want, got any
		_ = json.Unmarshal(c.Graph, &want)
		b, _ := json.Marshal(g)
		_ = json.Unmarshal(b, &got)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("case %d (%s): graphs differ\nwant %s\ngot  %s", i, c.Kind, c.Graph, b)
		}
	}
}

func TestComfyMustBeLocal(t *testing.T) {
	if _, err := New("http://192.168.1.5:8188"); err == nil {
		t.Fatal("a ComfyUI on the network was accepted")
	}
	if _, err := New("http://127.0.0.1:8188"); err != nil {
		t.Fatal(err)
	}
}
