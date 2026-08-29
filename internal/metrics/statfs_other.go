//go:build !linux

package metrics

type syscallStatfs struct {
	Bsize  int64
	Blocks uint64
	Bavail uint64
}

func statfs(path string, buf *syscallStatfs) error {
	// Fallback stub for non-Linux systems (e.g., macOS local development)
	// This ensures your IDE doesn't complain about undefined symbols.
	buf.Bsize = 4096
	buf.Blocks = 0
	buf.Bavail = 0
	return nil
}
