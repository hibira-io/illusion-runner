// Package gpu finds this machine's graphics cards and memory.
package gpu

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hibira-io/illusion-runner/internal/api"
)

// Detect lists the GPUs: NVIDIA cards through nvidia-smi; on Apple Silicon one
// GPU sharing the unified memory (macOS lets the GPU use about 75% of it).
func Detect() []api.GPU {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if mb := RAMMB(); mb > 0 {
			return []api.GPU{{Index: 0, Name: appleChip(), VRAMMB: mb * 3 / 4}}
		}
		return nil
	}
	out, err := run("nvidia-smi", "--query-gpu=index,name,memory.total", "--format=csv,noheader,nounits")
	if err != nil {
		return nil
	}
	return ParseNvidiaSMI(out)
}

// ParseNvidiaSMI reads `nvidia-smi --query-gpu=index,name,memory.total --format=csv,noheader,nounits`.
func ParseNvidiaSMI(out string) []api.GPU {
	var gpus []api.GPU
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		idx, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		mem, err2 := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1]))
		if err1 != nil || err2 != nil {
			continue
		}
		name := strings.TrimSpace(strings.Join(parts[1:len(parts)-1], ","))
		gpus = append(gpus, api.GPU{Index: idx, Name: name, VRAMMB: mem})
	}
	return gpus
}

// RAMMB is the machine's memory in MB (0 when unknown).
func RAMMB() int {
	switch runtime.GOOS {
	case "darwin":
		out, err := run("sysctl", "-n", "hw.memsize")
		if err != nil {
			return 0
		}
		b, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
		return int(b >> 20)
	case "linux":
		out, err := run("sh", "-c", "grep MemTotal /proc/meminfo")
		if err != nil {
			return 0
		}
		f := strings.Fields(out)
		if len(f) >= 2 {
			kb, _ := strconv.Atoi(f[1])
			return kb >> 10
		}
	case "windows":
		out, err := run("powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory")
		if err != nil {
			return 0
		}
		b, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
		return int(b >> 20)
	}
	return 0
}

func appleChip() string {
	out, err := run("sysctl", "-n", "machdep.cpu.brand_string")
	if err != nil || strings.TrimSpace(out) == "" {
		return "Apple GPU"
	}
	return strings.TrimSpace(out)
}

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}
