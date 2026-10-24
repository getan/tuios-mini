// Package control is the JSON-line client for a tuios-mini daemon.
//
// It is separate from package mini so a caller can speak the protocol without
// linking the daemon, its PTYs and the terminal emulator. It uses only the
// standard library.
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

const (
	// Version is what this client reports in the hello handshake.
	Version = "mini-control"

	// protocolVersion is the verb protocol this client speaks.
	protocolVersion = 1

	// helloVerb is the handshake verb. codeUnknownVerb is the one answer that
	// marks a daemon older than the handshake rather than a broken one.
	helloVerb       = "hello"
	codeUnknownVerb = "unknown_verb"

	// readChunk is how much one read from the socket may add to a response
	// line before the frame limit is checked again.
	readChunk = 4 << 10

	// dialTimeout bounds one dial attempt.
	dialTimeout = 3 * time.Second

	// handshakeTimeout bounds dialing and hello together when the caller's
	// context carries no deadline of its own.
	handshakeTimeout = 5 * time.Second

	// dialRetryBudget is DialRetry's startup budget. It covers every attempt
	// end to end, handshake included.
	dialRetryBudget = 5 * time.Second

	// dialRetryInterval is the pause between DialRetry's attempts.
	dialRetryInterval = 25 * time.Millisecond
)

// The two frame limits and the fallback call lifetime are variables rather than
// constants only so a test can shrink them. Nothing in the package writes them.
var (
	// maxResponseBytes is the largest response line this client assembles. It
	// is enforced as the line arrives, so a peer that never sends a newline
	// cannot make the client allocate more than this.
	maxResponseBytes = 16 << 20

	// maxRequestBytes is the largest request line this client sends. A
	// parameter is caller data and the daemon holds a request in memory, so it
	// is capped for the same reason a response is.
	maxRequestBytes = 16 << 20

	// defaultCallTTL bounds a call whose context carries no deadline.
	defaultCallTTL = 30 * time.Second
)

// Error is a verb failure as the daemon reports it.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return "mini: unknown error"
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

var (
	// errClientClosed is what every call returns after Close.
	errClientClosed = errors.New("mini: client is closed")

	// errResponseTooLarge reports a response line past maxResponseBytes. It is
	// replaced by a message naming the verb and the limit before it is
	// returned to a caller.
	errResponseTooLarge = errors.New("mini: response exceeds the frame limit")
)

// dialFunc opens one connection to the daemon socket.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func defaultDial(ctx context.Context, network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	return d.DialContext(ctx, network, addr)
}

// Client speaks the daemon's JSON-line control protocol. It is deliberately
// small: M1 needs the wire contract golder uses, not a facade over every verb.
//
// One call is in flight at a time, and a call waiting for its turn respects its
// context, so a canceled caller never waits for the call ahead of it. A
// connection that fails a call is discarded, and the next call dials again and
// repeats hello, so a late answer to the failed call can never be read as the
// answer to the next one. A call that failed is never sent again
// automatically, because the daemon may already have carried it out.
type Client struct {
	socket string
	dial   dialFunc
	sem    chan struct{}

	mu     sync.Mutex
	conn   net.Conn
	reader *lineReader
	nextID int
	closed bool
}

// Dial connects to socket and completes the hello handshake.
//
// hello is advisory in this protocol: an older daemon answers unknown_verb and
// is still usable. Every other handshake failure is reported, because a daemon
// that cannot answer the handshake will not serve the first verb either.
func Dial(ctx context.Context, socket string) (*Client, error) {
	return dial(ctx, socket, defaultDial)
}

func dial(ctx context.Context, socket string, fn dialFunc) (*Client, error) {
	if socket == "" {
		return nil, errors.New("mini: socket path is empty")
	}
	c := &Client{socket: socket, dial: fn, sem: make(chan struct{}, 1)}
	if c.dial == nil {
		c.dial = defaultDial
	}
	if _, _, err := c.connect(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Close closes the connection and refuses every later call. It is safe to call
// more than once and from any goroutine: a call in flight fails rather than
// continuing, and nothing dials again afterwards.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.closed = true
	conn := c.conn
	c.conn, c.reader = nil, nil
	c.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
}

// Call invokes one verb and returns its raw result. The context bounds the work
// and not only the wait: canceling it interrupts a dial, a write or a read in
// flight, and its deadline bounds the round trip (a call with no deadline gets
// defaultCallTTL).
//
// A call whose wire failed is not retried here. The request may have reached
// the daemon and been carried out before the answer was lost, so replaying a
// mutation would apply it twice; the connection is discarded instead, and the
// caller decides whether to send the verb again.
func (c *Client) Call(ctx context.Context, verb string, params any) (json.RawMessage, error) {
	if c == nil {
		return nil, errors.New("mini: nil client")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-c.sem }()

	conn, reader, err := c.connection(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.exchange(ctx, conn, reader, verb, params)
	switch {
	case err == nil:
		if ctx.Err() != nil {
			// The answer is good, but the cancellation squeezed this
			// connection's deadline; the next call starts on a fresh one.
			c.discard(conn)
		}
		return raw, nil
	case isDaemonError(err):
		// The daemon answered, so the connection is healthy and stays.
		return nil, err
	case isUnwritten(err):
		// Nothing reached the wire: the request could not be built or was too
		// large. The connection is exactly as it was.
		return nil, err
	default:
		c.discard(conn)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if c.isClosed() {
			return nil, errClientClosed
		}
		return nil, err
	}
}

// acquire waits for this client's one call slot, giving up when ctx does. A
// mutex cannot be interrupted, and a caller that cancels must not be made to
// wait for the call ahead of it, which may be a wait-for that runs for minutes.
func (c *Client) acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// connection returns the live connection, connecting first when the previous
// one was discarded.
func (c *Client) connection(ctx context.Context) (net.Conn, *lineReader, error) {
	c.mu.Lock()
	conn, reader, closed := c.conn, c.reader, c.closed
	c.mu.Unlock()
	if closed {
		return nil, nil, errClientClosed
	}
	if conn != nil {
		return conn, reader, nil
	}
	return c.connect(ctx)
}

// connect dials, installs the connection and completes hello. The caller holds
// the call slot.
func (c *Client) connect(ctx context.Context) (net.Conn, *lineReader, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, nil, errClientClosed
	}

	dialCtx, cancel := bounded(ctx, handshakeTimeout)
	defer cancel()

	conn, err := c.dial(dialCtx, "unix", c.socket)
	if err != nil {
		return nil, nil, fmt.Errorf("mini: connect %s: %w", c.socket, err)
	}
	reader := &lineReader{conn: conn}
	if err := c.install(conn, reader); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if _, err := c.exchange(dialCtx, conn, reader, helloVerb, helloParams()); err != nil {
		var de *Error
		if !errors.As(err, &de) || de.Code != codeUnknownVerb {
			c.discard(conn)
			if c.isClosed() {
				return nil, nil, errClientClosed
			}
			return nil, nil, fmt.Errorf("mini: hello %s: %w", c.socket, err)
		}
	}
	return conn, reader, nil
}

// helloParams is the handshake every connection opens with.
func helloParams() map[string]any {
	return map[string]any{
		"client": "tuios-mini", "version": Version, "protocol": protocolVersion,
	}
}

// install makes conn the client's connection. A Close that raced the dial wins.
func (c *Client) install(conn net.Conn, reader *lineReader) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errClientClosed
	}
	old := c.conn
	c.conn, c.reader = conn, reader
	if old != nil && old != conn {
		_ = old.Close()
	}
	return nil
}

// discard closes conn and forgets it, unless Close or a later connection
// already replaced it.
func (c *Client) discard(conn net.Conn) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn, c.reader = nil, nil
	}
	c.mu.Unlock()
	_ = conn.Close()
}

func (c *Client) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// exchange sends one request on conn and reads exactly one response. A failure
// the daemon reported comes back as *Error; every other error means the
// connection can no longer be trusted, because the answer may still arrive.
func (c *Client) exchange(ctx context.Context, conn net.Conn, reader *lineReader, verb string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	id := c.nextID + 1
	c.mu.Unlock()

	req := struct {
		ID     int    `json:"id"`
		Verb   string `json:"verb"`
		Params any    `json:"params,omitempty"`
	}{ID: id, Verb: verb, Params: params}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, &unwrittenError{fmt.Errorf("mini: encode %s: %w", verb, err)}
	}
	line = append(line, '\n')
	if len(line) > maxRequestBytes {
		return nil, &unwrittenError{fmt.Errorf("mini: %s request of %d bytes exceeds the %d byte limit", verb, len(line), maxRequestBytes)}
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultCallTTL)
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errClientClosed
	}
	c.nextID = id
	c.mu.Unlock()

	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("mini: %s: %w", verb, err)
	}
	// The deadline can be minutes away, and a cancellation has to interrupt a
	// blocked read now rather than at the end of a wait-for. Squeezing the
	// deadline is the portable way to break a read already in flight.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer func() {
		if stop() {
			_ = conn.SetDeadline(time.Time{})
		}
	}()

	if err := writeAll(conn, line); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("mini: write %s: %w", verb, err)
	}

	reply, err := reader.next(maxResponseBytes)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		switch {
		case errors.Is(err, errResponseTooLarge):
			return nil, fmt.Errorf("mini: %s response exceeds the %d byte limit", verb, maxResponseBytes)
		case isTimeout(err):
			return nil, fmt.Errorf("mini: %s timed out: %w", verb, context.DeadlineExceeded)
		default:
			return nil, fmt.Errorf("mini: read %s: %w", verb, err)
		}
	}

	var resp struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(reply), &resp); err != nil {
		return nil, fmt.Errorf("mini: decode %s: %w", verb, err)
	}
	if resp.ID != id {
		return nil, fmt.Errorf("mini: %s response id %d, want %d", verb, resp.ID, id)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// unwrittenError is a failure that happened before a request reached the
// connection: a parameter that would not encode, or a request over the frame
// limit. The connection is untouched, so it stays usable.
type unwrittenError struct{ err error }

func (e *unwrittenError) Error() string { return e.err.Error() }

func (e *unwrittenError) Unwrap() error { return e.err }

// isUnwritten reports whether err happened before anything was written.
func isUnwritten(err error) bool {
	var ue *unwrittenError
	return errors.As(err, &ue)
}

// lineReader reads one response line at a time from a connection.
//
// It deliberately keeps its own buffer instead of handing the connection to
// bufio: bufio's line read hides how much of the stream it consumed, and a read
// that swallows two lines silently discards the second. That second line is
// exactly the stale answer this client has to see and reject by id, so the
// reader returns one line and leaves everything after it buffered.
type lineReader struct {
	conn net.Conn
	buf  []byte
}

// next returns the next line, newline included, refusing to assemble more than
// limit bytes. The limit is checked as bytes arrive, so a peer that never sends
// a newline is cut off at the limit instead of being buffered without bound.
func (r *lineReader) next(limit int) ([]byte, error) {
	for {
		if i := bytes.IndexByte(r.buf, '\n'); i >= 0 {
			if i+1 > limit {
				return nil, errResponseTooLarge
			}
			line := make([]byte, i+1)
			copy(line, r.buf[:i+1])
			r.buf = r.buf[i+1:]
			return line, nil
		}
		if len(r.buf) > limit {
			return nil, errResponseTooLarge
		}
		var chunk [readChunk]byte
		n, err := r.conn.Read(chunk[:])
		if n > 0 {
			r.buf = append(r.buf, chunk[:n]...)
		}
		if err != nil {
			return nil, err
		}
	}
}

// writeAll hands every byte to the connection. A net.Conn may write short with
// a nil error, and treating the first Write as the whole request would cut it
// at exactly the size where it stops fitting a buffer.
func writeAll(conn net.Conn, b []byte) error {
	for len(b) > 0 {
		n, err := conn.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

// isDaemonError reports whether err is an answer the daemon gave, which leaves
// the connection healthy.
func isDaemonError(err error) bool {
	var de *Error
	return errors.As(err, &de)
}

// isTimeout reports whether err is a deadline the connection hit.
func isTimeout(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// bounded returns ctx with a timeout when it carries no deadline of its own.
func bounded(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

// DialRetry waits for a daemon that is still binding its socket.
//
// The five-second budget is a budget for the whole wait, not for each attempt:
// a daemon that accepts a connection and then says nothing during hello cannot
// stretch it. A deadline or cancellation on ctx wins over the budget. A daemon
// that answered the handshake with a protocol error is not retried, because it
// will answer the same way a moment later.
func DialRetry(ctx context.Context, socket string) (*Client, error) {
	return dialRetry(ctx, socket, defaultDial, dialRetryBudget, dialRetryInterval)
}

func dialRetry(ctx context.Context, socket string, fn dialFunc, budget, interval time.Duration) (*Client, error) {
	deadline := time.Now().Add(budget)
	var last error
	for {
		if last != nil && !time.Now().Before(deadline) {
			return nil, fmt.Errorf("mini: dial %s: %w", socket, last)
		}
		attemptCtx, cancel := context.WithDeadline(ctx, deadline)
		c, err := dial(attemptCtx, socket, fn)
		cancel()
		if err == nil {
			return c, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if isDaemonError(err) {
			// The daemon spoke and refused the handshake on purpose.
			return nil, err
		}
		last = err
		wait := min(interval, time.Until(deadline))
		if wait <= 0 {
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
