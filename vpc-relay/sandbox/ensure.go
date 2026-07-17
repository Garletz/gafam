package sandbox

import (
	"log"
	"os"
	"path/filepath"
)

// EnsureDirs creates the host-side sandbox directories and makes them
// writable by the in-container `sandbox` user (uid 1000). Without this, the
// bind-mounted dirs stay root-owned and every write inside the sandbox
// (vault notes, uploads, mission archives) fails with EACCES.
func EnsureDirs() {
	base := "/root/gafam_data/sandbox"
	dirs := []string{
		"files", "downloads", "screenshots", "scripts", "tmp",
		"files/research", "files/research/notes", "files/research/missions",
		"files/missions",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(base, d), 0o777); err != nil {
			log.Printf("sandbox: EnsureDirs mkdir %s: %v", d, err)
		}
	}
	// Chown the whole tree once at startup (idempotent).
	_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err == nil {
			_ = os.Chown(path, 1000, 1000)
		}
		return nil
	})
	log.Println("sandbox: host dirs ready (uid 1000 writable)")
}
