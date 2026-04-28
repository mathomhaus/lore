// probeLibrary: find a usable libonnxruntime on the current unix system.
// Lore does not bundle the native ONNX Runtime shared library; it bundles
// only the model and vocabulary files. The shared library must be installed
// on the host (e.g. via Homebrew, apt, or the onnxruntime GitHub release).
//
// Search order:
//  1. LORE_ONNXRUNTIME_LIB environment variable (explicit override)
//  2. Default system paths per GOOS (darwin vs linux)

//go:build unix

package bge

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"
)

// loreLibEnv is the environment variable callers may set to override the
// default library search. Absolute path to libonnxruntime.{dylib,so}.
const loreLibEnv = "LORE_ONNXRUNTIME_LIB"

// defaultLibCandidates returns the ordered list of candidate paths to try
// for the current GOOS.
func defaultLibCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			// Homebrew arm64 (Apple Silicon)
			"/opt/homebrew/lib/libonnxruntime.dylib",
			// Homebrew x86
			"/usr/local/lib/libonnxruntime.dylib",
			// Manual install
			"/usr/lib/libonnxruntime.dylib",
		}
	case "linux":
		return []string{
			"/usr/lib/libonnxruntime.so",
			"/usr/local/lib/libonnxruntime.so",
			"/usr/lib/x86_64-linux-gnu/libonnxruntime.so",
			"/usr/lib/aarch64-linux-gnu/libonnxruntime.so",
		}
	default:
		return nil
	}
}

// probeLibrary returns the path to a usable libonnxruntime. Returns an
// error wrapping ErrUnsupported if no candidate is found.
func probeLibrary(logger *slog.Logger) (string, error) {
	if v := os.Getenv(loreLibEnv); v != "" {
		if _, err := os.Stat(v); err != nil {
			return "", fmt.Errorf("LORE_ONNXRUNTIME_LIB=%q: stat: %w", v, err)
		}
		logger.Debug("bge: using ONNX runtime from env", "path", v)
		return v, nil
	}
	candidates := defaultLibCandidates()
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			logger.Debug("bge: found ONNX runtime library", "path", c)
			return c, nil
		}
	}
	return "", fmt.Errorf("libonnxruntime not found; install it or set %s", loreLibEnv)
}
