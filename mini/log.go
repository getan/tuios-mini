package mini

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// instanceLog is one embedded daemon's own log file.
//
// It exists because a log file has an owner. The standalone daemon appends to
// one file for the whole process, through the standard library logger, and
// takes that logger over to do it (session.InstallDaemonLogging). An embedding
// program cannot afford either: two daemons in one process would share one
// file, and the program's own log output would start landing in a workspace's
// log. So mini opens the file itself, hands the daemon a sink that writes here,
// and leaves log.SetOutput alone.
//
// The format is the standalone daemon's, so one reader still works: a header
// naming what the file is, then one line per entry with its level and time.
type instanceLog struct {
	path string

	mu   sync.Mutex
	fh   *os.File
	size int64
}

// logRotateBytes caps a workspace log. At the cap the file is renamed over
// .old and a new one is started, so a daemon that runs for months keeps two
// caps of history and never grows without bound.
const logRotateBytes = 4 << 20

// openInstanceLog opens path for this daemon and writes the header that says
// what the file holds.
func openInstanceLog(path, version, socket string) (*instanceLog, error) {
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("mini: open the log file %s: %w", path, err)
	}
	l := &instanceLog{path: path, fh: fh}
	if st, serr := fh.Stat(); serr == nil {
		l.size = st.Size()
	}
	l.writeLocked("info", fmt.Sprintf("=== tuios-mini log started %s pid=%d version=%s socket=%s ===",
		time.Now().Format(time.RFC3339), os.Getpid(), version, socket))
	return l, nil
}

// write appends one entry. It is the sink the daemon is given, so it is called
// from the daemon's own goroutines and must be safe for concurrent use. A write
// that fails is dropped: a daemon must not stop over its own log.
func (l *instanceLog) write(level, message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writeLocked(level, message)
}

func (l *instanceLog) writeLocked(level, message string) {
	if l.fh == nil {
		return
	}
	line := fmt.Sprintf("%s [%s] %s\n", time.Now().Format("2006-01-02T15:04:05.000"), level, message)
	n, err := l.fh.WriteString(line)
	if err != nil {
		return
	}
	l.size += int64(n)
	if l.size >= logRotateBytes {
		l.rotateLocked()
	}
}

// rotateLocked moves the full file aside and starts a new one with the same
// header shape, so the boundary is visible rather than a gap.
func (l *instanceLog) rotateLocked() {
	_ = l.fh.Close()
	l.fh = nil
	if err := os.Rename(l.path, l.path+".old"); err != nil {
		// Keep appending rather than losing the rest of the run.
		if fh, oerr := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); oerr == nil {
			l.fh = fh
		}
		return
	}
	fh, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	l.fh = fh
	l.size = 0
	l.writeLocked("info", "=== log rotated; the previous file is "+l.path+".old ===")
}

// close releases the file. A later write is dropped rather than reopening it:
// a daemon that has stopped has nothing more to say.
func (l *instanceLog) close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fh == nil {
		return nil
	}
	err := l.fh.Close()
	l.fh = nil
	return err
}
