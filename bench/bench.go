// Package bench provides benchmarking utilities for HotStuff-2.
package bench

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// SystemInfo contains system information for the benchmark report.
type SystemInfo struct {
	Timestamp    string
	OS           string
	Architecture string
	GoVersion    string
	CPU          string
	NumCPU       int
}

// GetSystemInfo retrieves current system information.
func GetSystemInfo() *SystemInfo {
	info := &SystemInfo{
		Timestamp:    time.Now().Format("2006-01-02 15:04:05 MST"),
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
	}

	// Get Go version
	if output, err := exec.Command("go", "version").Output(); err == nil {
		parts := strings.Fields(string(output))
		if len(parts) >= 3 {
			info.GoVersion = parts[2] // e.g., "go1.21.0"
		} else {
			info.GoVersion = strings.TrimSpace(string(output))
		}
	}

	// Get CPU info (Linux)
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "model name") {
					parts := strings.Split(line, ":")
					if len(parts) > 1 {
						info.CPU = strings.TrimSpace(parts[1])
						break
					}
				}
			}
		}
	}

	// macOS CPU info
	if runtime.GOOS == "darwin" {
		if output, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			info.CPU = strings.TrimSpace(string(output))
		}
	}

	// Windows CPU info
	if runtime.GOOS == "windows" {
		if output, err := exec.Command("wmic", "cpu", "get", "name").Output(); err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" && line != "Name" {
					info.CPU = line
					break
				}
			}
		}
	}

	// Fallback for unknown OS
	if info.CPU == "" {
		info.CPU = fmt.Sprintf("%s/%s (%d cores)", runtime.GOOS, runtime.GOARCH, info.NumCPU)
	}

	return info
}

// BenchmarkResult represents a single benchmark result.
type BenchmarkResult struct {
	Name        string
	Iterations  int64
	NsPerOp     float64
	BytesPerOp  int64
	AllocsPerOp int64
}

// ParseBenchmarkOutput parses Go benchmark output into structured results.
func ParseBenchmarkOutput(output string) []BenchmarkResult {
	results := []BenchmarkResult{}
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		result := BenchmarkResult{
			Name: fields[0],
		}

		// Parse iterations
		if len(fields) > 1 {
			if n, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				result.Iterations = n
			}
		}

		// Parse remaining fields
		for i := 2; i < len(fields); i++ {
			field := fields[i]

			// ns/op - could be integer or float
			if i+1 < len(fields) && fields[i+1] == "ns/op" {
				if ns, err := strconv.ParseFloat(field, 64); err == nil {
					result.NsPerOp = ns
				}
				i++
				continue
			}

			// B/op
			if i+1 < len(fields) && fields[i+1] == "B/op" {
				if b, err := strconv.ParseInt(field, 10, 64); err == nil {
					result.BytesPerOp = b
				}
				i++
				continue
			}

			// allocs/op
			if i+1 < len(fields) && fields[i+1] == "allocs/op" {
				if a, err := strconv.ParseInt(field, 10, 64); err == nil {
					result.AllocsPerOp = a
				}
				i++
				continue
			}
		}

		if result.NsPerOp > 0 {
			results = append(results, result)
		}
	}

	return results
}

// ClassifySpeed classifies benchmark speed for HTML coloring.
// Returns CSS class name based on operation speed.
func ClassifySpeed(nsPerOp float64) string {
	switch {
	case nsPerOp < 1000: // < 1μs
		return "metric-fast"
	case nsPerOp < 100000: // < 100μs
		return "metric-fast"
	case nsPerOp < 1000000: // < 1ms
		return "metric-medium"
	default:
		return "metric-slow"
	}
}

// FormatDuration formats nanoseconds into a human-readable duration.
func FormatDuration(ns float64) string {
	switch {
	case ns < 1000:
		return fmt.Sprintf("%.1f ns", ns)
	case ns < 1000000:
		return fmt.Sprintf("%.2f μs", ns/1000)
	case ns < 1000000000:
		return fmt.Sprintf("%.2f ms", ns/1000000)
	default:
		return fmt.Sprintf("%.2f s", ns/1000000000)
	}
}

// FormatOpsPerSec calculates and formats operations per second.
func FormatOpsPerSec(nsPerOp float64) string {
	if nsPerOp == 0 {
		return "N/A"
	}
	opsPerSec := 1000000000 / nsPerOp

	switch {
	case opsPerSec >= 1000000:
		return fmt.Sprintf("%.2fM", opsPerSec/1000000)
	case opsPerSec >= 1000:
		return fmt.Sprintf("%.1fK", opsPerSec/1000)
	default:
		return fmt.Sprintf("%.0f", opsPerSec)
	}
}

// CategoryBadge returns the HTML badge for a benchmark category.
func CategoryBadge(category string) string {
	category = strings.ToLower(category)

	switch {
	case strings.Contains(category, "crypto"):
		return `<span class="badge badge-crypto">CRYPTO</span>`
	case strings.Contains(category, "qc"):
		return `<span class="badge badge-qc">QC</span>`
	case strings.Contains(category, "vote"):
		return `<span class="badge badge-vote">VOTE</span>`
	case strings.Contains(category, "timer"):
		return `<span class="badge badge-timer">TIMER</span>`
	case strings.Contains(category, "consensus"):
		return `<span class="badge badge-consensus">CONSENSUS</span>`
	default:
		return `<span class="badge badge-qc">OTHER</span>`
	}
}
