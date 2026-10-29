package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// startupTrace records the wall-clock cost of the attach path. It is written
// inside the client's own state directory, not the person's, and is small
// enough to leave on every run: one line per phase.
//
// The first frame itself is not in here. This trace ends at p.Run; the phases
// before it are the ones that can keep the program from starting at all.
type startupTrace struct {
	start time.Time
	file  *os.File
}

func beginStartupTrace(dir string) *startupTrace {
	t := &startupTrace{start: time.Now()}
	if dir == "" {
		return t
	}
	f, err := os.OpenFile(filepath.Join(dir, "startup.log"),
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return t
	}
	t.file = f
	_, _ = fmt.Fprintf(f, "startup trace started %s\n", t.start.Format(time.RFC3339Nano))
	return t
}

func (t *startupTrace) mark(phase string) {
	if t == nil || t.file == nil {
		return
	}
	_, _ = fmt.Fprintf(t.file, "%-24s %10.3f ms\n", phase, float64(time.Since(t.start))/float64(time.Millisecond))
}

func (t *startupTrace) close() {
	if t != nil && t.file != nil {
		_ = t.file.Close()
	}
}
