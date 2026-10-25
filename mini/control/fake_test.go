package control

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A control test needs a daemon that answers exactly what the test says, when
// the test says it, and a transport that cannot deadlock. These helpers give
// either one:
//
//   - newPipeDaemon serves over net.Pipe: no socket, no kernel buffer, and a
//     transport that pairs every write with a read. It is the right choice when
//     the scripted answer and the client's call strictly alternate.
//   - newSocketDaemon serves over a real Unix socket, which a test needs when
//     the daemon writes ahead of the next request (a stale line) or when the
//     test wants the client's own dialer rather than net.Pipe.

// request is one request line as the fake daemon read it.
type request struct {
	ID     int             `json:"id"`
	Verb   string          `json:"verb"`
	Params json.RawMessage `json:"params"`
}

// fakeDaemon is a scripted daemon. serve runs once per connection and receives
// the connection number, starting at 1.
type fakeDaemon struct {
	serve func(d *fakeDaemon, n int, c net.Conn)

	// path is the socket a socket daemon listens on, empty for a pipe daemon.
	path string

	mu    sync.Mutex
	reqs  []request
	conns int
}

// newPipeDaemon returns a daemon whose dial hands the client one end of a
// net.Pipe and runs serve on the other end.
func newPipeDaemon(t *testing.T, serve func(d *fakeDaemon, n int, c net.Conn)) *fakeDaemon {
	t.Helper()
	return &fakeDaemon{serve: serve}
}

// newSocketDaemon returns a daemon listening on a Unix socket in the process's
// temp directory. The path is kept short because the kernel's socket address is
// length limited and t.TempDir, which appends the test's own name, is over that
// limit on macOS; a bare MkdirTemp name is not.
func newSocketDaemon(t *testing.T, serve func(d *fakeDaemon, n int, c net.Conn)) *fakeDaemon {
	t.Helper()
	dir, err := os.MkdirTemp("", "mini-ctl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	if len(sock) >= 104 {
		t.Fatalf("socket path %s is %d bytes, over the kernel's limit", sock, len(sock))
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	d := &fakeDaemon{serve: serve, path: sock}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			n := d.countConn()
			go func() {
				defer func() { _ = conn.Close() }()
				d.serve(d, n, conn)
			}()
		}
	}()
	return d
}

func (d *fakeDaemon) countConn() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.conns++
	return d.conns
}

// dial is the client's dial function for a pipe daemon.
func (d *fakeDaemon) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n := d.countConn()
	client, server := net.Pipe()
	go func() {
		defer func() { _ = server.Close() }()
		d.serve(d, n, server)
	}()
	return client, nil
}

// connectionCount is how many connections the daemon has accepted.
func (d *fakeDaemon) connectionCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conns
}

// record notes one request the daemon read.
func (d *fakeDaemon) record(req request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reqs = append(d.reqs, req)
}

// loop reads request lines from c and calls handle for each of them. It returns
// when the client hangs up or the request line is unusable.
func (d *fakeDaemon) loop(c net.Conn, handle func(req request)) {
	r := bufio.NewReader(c)
	for {
		req, err := readRequest(r)
		if err != nil {
			return
		}
		d.record(req)
		handle(req)
	}
}

// readOneThenHangUp reads a single request and closes the connection without
// answering it, which is what a daemon too old to speak this protocol at all
// does with a request line.
func (d *fakeDaemon) readOneThenHangUp(c net.Conn) {
	req, err := readRequest(bufio.NewReader(c))
	if err == nil {
		d.record(req)
	}
}

// verbs waits until the daemon has read at least n requests and returns every
// verb it read, in order. Waiting matters: a request is recorded by the
// daemon's goroutine, which can trail the call that returned.
func (d *fakeDaemon) verbs(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		d.mu.Lock()
		got := make([]string, len(d.reqs))
		for i, req := range d.reqs {
			got[i] = req.Verb
		}
		d.mu.Unlock()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon read %d requests %v, want %d", len(got), got, n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// requestAt returns the i'th request the daemon read.
func (d *fakeDaemon) requestAt(t *testing.T, i int) request {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if i >= len(d.reqs) {
		t.Fatalf("the daemon read %d requests, want at least %d", len(d.reqs), i+1)
	}
	return d.reqs[i]
}

// readRequest reads exactly one request line.
func readRequest(r *bufio.Reader) (request, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return request{}, err
	}
	var req request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return request{}, err
	}
	return req, nil
}

// writeLine writes one raw response line. A write error means the client has
// already hung up, which is how most of these tests end a connection.
func writeLine(c net.Conn, line []byte) {
	_, _ = c.Write(line)
}

// writeResult answers a request with a result.
func writeResult(c net.Conn, id int, result any) {
	b, err := json.Marshal(map[string]any{"id": id, "result": result})
	if err != nil {
		return
	}
	writeLine(c, append(b, '\n'))
}

// writeDaemonError answers a request with a verb error envelope.
func writeDaemonError(c net.Conn, id int, code, message string) {
	b, err := json.Marshal(map[string]any{
		"id":    id,
		"error": map[string]string{"code": code, "message": message},
	})
	if err != nil {
		return
	}
	writeLine(c, append(b, '\n'))
}

// sizedLine returns a valid response line of exactly n bytes, newline included,
// so a test can sit on the frame limit's boundary.
func sizedLine(n, id int) []byte {
	prefix := fmt.Sprintf(`{"id":%d,"result":"`, id)
	suffix := "\"}\n"
	if n < len(prefix)+len(suffix) {
		n = len(prefix) + len(suffix)
	}
	return []byte(prefix + strings.Repeat("x", n-len(prefix)-len(suffix)) + suffix)
}

// shortWriter writes at most limit bytes per call. A net.Conn is allowed to
// write short with a nil error, and this is that connection.
type shortWriter struct {
	net.Conn
	limit int
}

func (s *shortWriter) Write(p []byte) (int, error) {
	if len(p) > s.limit {
		p = p[:s.limit]
	}
	return s.Conn.Write(p)
}

// countingDial counts attempts and delegates to fn.
type countingDial struct {
	mu       sync.Mutex
	attempts int
	fn       dialFunc
}

func (c *countingDial) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	c.mu.Lock()
	c.attempts++
	c.mu.Unlock()
	return c.fn(ctx, network, addr)
}

func (c *countingDial) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts
}

// serveHello answers the handshake and leaves every other verb to handle.
func serveHello(d *fakeDaemon, c net.Conn, handle func(req request)) {
	d.loop(c, func(req request) {
		if req.Verb == helloVerb {
			writeResult(c, req.ID, map[string]any{"type": "hello", "protocol": protocolVersion})
			return
		}
		handle(req)
	})
}
