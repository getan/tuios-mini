// Package control is the JSON-line client for a tuios-mini daemon.
//
// It is separate from package mini so a caller can speak the protocol without
// linking the daemon, its PTYs and the terminal emulator.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	maxResponseBytes = 16 << 20
	// Version is what this client reports in the hello handshake.
	Version = "mini-control"
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

// Client speaks the daemon's JSON-line control protocol. It is deliberately
// small: M1 needs the wire contract golder uses, not a facade over every verb.
type Client struct {
	conn   net.Conn
	reader *bufio.Reader

	mu     sync.Mutex
	nextID int
}

// Dial connects to socket and completes the optional hello handshake.
func Dial(ctx context.Context, socket string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, reader: bufio.NewReader(conn)}
	// hello is optional for the daemon, and older peers answer unknown_verb.
	// A failure here is not a failure to connect; the first real verb reports
	// an incompatible daemon with its own error.
	_, _ = c.Call(ctx, "hello", map[string]any{
		"client": "tuios-mini", "version": Version, "protocol": 1,
	})
	return c, nil
}

// Call invokes one verb and returns its raw result.
func (c *Client) Call(ctx context.Context, verb string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	_ = c.conn.SetDeadline(deadline)

	c.nextID++
	req := struct {
		ID     int    `json:"id"`
		Verb   string `json:"verb"`
		Params any    `json:"params,omitempty"`
	}{ID: c.nextID, Verb: verb, Params: params}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mini: encode %s: %w", verb, err)
	}
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		return nil, fmt.Errorf("mini: write %s: %w", verb, err)
	}
	reply, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("mini: read %s: %w", verb, err)
	}
	if len(reply) > maxResponseBytes {
		return nil, errors.New("mini: response too large")
	}
	var resp struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(reply, &resp); err != nil {
		return nil, fmt.Errorf("mini: decode %s: %w", verb, err)
	}
	if resp.ID != c.nextID {
		return nil, fmt.Errorf("mini: %s response id %d, want %d", verb, resp.ID, c.nextID)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// DialRetry waits for a daemon that is still binding its socket.
func DialRetry(ctx context.Context, socket string) (*Client, error) {
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for {
		c, err := Dial(ctx, socket)
		if err == nil {
			return c, nil
		}
		last = err
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("mini: dial %s: %w", socket, last)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
