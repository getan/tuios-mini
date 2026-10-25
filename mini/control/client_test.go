package control

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTheWireContractLimits pins the numbers the rest of the tree assumes. The
// two frame limits are variables only so the tests below can shrink them.
func TestTheWireContractLimits(t *testing.T) {
	if maxResponseBytes != 16<<20 {
		t.Errorf("maxResponseBytes = %d, want 16MiB", maxResponseBytes)
	}
	if maxRequestBytes != 16<<20 {
		t.Errorf("maxRequestBytes = %d, want 16MiB", maxRequestBytes)
	}
	if dialRetryBudget != 5*time.Second {
		t.Errorf("dialRetryBudget = %s, want the five-second startup budget", dialRetryBudget)
	}
}

// TestDialGreetsAndCalls drives the public Dial over a real Unix socket, so the
// handshake, the client's own dialer and the id sequence are all exercised the
// way a caller uses them.
func TestDialGreetsAndCalls(t *testing.T) {
	d := newSocketDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			switch req.Verb {
			case "echo":
				writeResult(c, req.ID, req.Params)
			default:
				writeDaemonError(c, req.ID, codeUnknownVerb, "unknown verb")
			}
		})
	})

	c, err := Dial(context.Background(), d.path)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	raw, err := c.Call(context.Background(), "echo", map[string]any{"session": "work"})
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	var echoed map[string]string
	if err := json.Unmarshal(raw, &echoed); err != nil {
		t.Fatalf("echo result %s: %v", raw, err)
	}
	if echoed["session"] != "work" {
		t.Fatalf("echo = %v, want the parameters back", echoed)
	}

	if verbs := d.verbs(t, 2); !slices.Equal(verbs, []string{helloVerb, "echo"}) {
		t.Fatalf("verbs = %v, want a greeting before the call", verbs)
	}
	if n := d.connectionCount(); n != 1 {
		t.Fatalf("%d connections, want every call on one", n)
	}

	hello := d.requestAt(t, 0)
	if hello.ID != 1 {
		t.Fatalf("hello id = %d, want 1", hello.ID)
	}
	var params struct {
		Client   string `json:"client"`
		Version  string `json:"version"`
		Protocol int    `json:"protocol"`
	}
	if err := json.Unmarshal(hello.Params, &params); err != nil {
		t.Fatalf("hello params %s: %v", hello.Params, err)
	}
	if params.Client != "tuios-mini" || params.Version != Version || params.Protocol != protocolVersion {
		t.Fatalf("hello params = %+v, want client tuios-mini version %s protocol %d",
			params, Version, protocolVersion)
	}
	if echo := d.requestAt(t, 1); echo.ID != 2 {
		t.Fatalf("echo id = %d, want 2 on the same connection", echo.ID)
	}
}

// TestDialAcceptsOnlyUnknownVerbAsALegacyHello is the compatibility contract:
// a daemon older than the handshake answers unknown_verb and is still usable,
// and every other handshake failure is a failure to connect. Ignoring the
// handshake wholesale, as the client used to, turned a protocol mismatch into a
// confusing error on the first real verb.
func TestDialAcceptsOnlyUnknownVerbAsALegacyHello(t *testing.T) {
	cases := []struct {
		name   string
		answer func(c net.Conn, req request)
		legacy bool
		hangup bool
		code   string
		want   string
	}{
		{
			name:   "unknown verb is an older daemon",
			answer: func(c net.Conn, req request) { writeDaemonError(c, req.ID, codeUnknownVerb, "unknown verb") },
			legacy: true,
		},
		{
			name: "a protocol mismatch is fatal",
			answer: func(c net.Conn, req request) {
				writeDaemonError(c, req.ID, "protocol_mismatch", "client speaks protocol 9")
			},
			code: "protocol_mismatch",
		},
		{
			name:   "invalid params are fatal",
			answer: func(c net.Conn, req request) { writeDaemonError(c, req.ID, "invalid_params", "no such parameter") },
			code:   "invalid_params",
		},
		{
			name:   "an undecodable answer is fatal",
			answer: func(c net.Conn, req request) { writeLine(c, []byte("not json\n")) },
			want:   "decode hello",
		},
		{
			name:   "an answer to another id is fatal",
			answer: func(c net.Conn, req request) { writeResult(c, req.ID+1, map[string]any{"protocol": protocolVersion}) },
			want:   "response id",
		},
		{
			name:   "a hangup is fatal",
			answer: func(c net.Conn, req request) {},
			hangup: true,
			want:   "hello",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
				if tc.hangup {
					d.readOneThenHangUp(c)
					return
				}
				d.loop(c, func(req request) {
					if req.Verb == helloVerb {
						tc.answer(c, req)
						return
					}
					writeResult(c, req.ID, map[string]any{"ok": true})
				})
			})

			c, err := dial(context.Background(), "fake", d.dial)
			if tc.legacy {
				if err != nil {
					t.Fatalf("Dial with a legacy unknown_verb answer = %v, want a usable client", err)
				}
				defer c.Close()
				if _, err := c.Call(context.Background(), "ping", nil); err != nil {
					t.Fatalf("call on a legacy connection = %v", err)
				}
				if n := d.connectionCount(); n != 1 {
					t.Fatalf("%d connections, want the legacy greeting kept", n)
				}
				return
			}
			if err == nil {
				_ = c.Close()
				t.Fatal("Dial accepted a handshake the daemon did not answer properly")
			}
			if tc.code != "" {
				var de *Error
				if !errors.As(err, &de) || de.Code != tc.code {
					t.Fatalf("Dial = %v, want the daemon's %s error", err, tc.code)
				}
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Dial = %v, want an error mentioning %q", err, tc.want)
			}
			if n := d.connectionCount(); n != 1 {
				t.Fatalf("a failed handshake dialed %d times, want 1", n)
			}
		})
	}
}

func TestDialRefusesAnEmptySocket(t *testing.T) {
	if _, err := Dial(context.Background(), ""); err == nil {
		t.Fatal("Dial accepted an empty socket path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	absent := filepath.Join(os.TempDir(), "mini-control-does-not-exist.sock")
	if _, err := dial(ctx, absent, nil); err == nil {
		t.Fatal("Dial connected to a socket that does not exist")
	}
}

// TestCallTimeoutDiscardsTheConnectionAndTheNextCallReconnects is the failure
// this change exists for. A timed-out call leaves its answer in flight; reusing
// the connection would read that stale line as the next call's answer. The
// client must drop the connection, greet the daemon again, and never resend the
// timed-out verb, which the daemon may already have carried out.
func TestCallTimeoutDiscardsTheConnectionAndTheNextCallReconnects(t *testing.T) {
	released := make(chan struct{})
	defer close(released)
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			switch req.Verb {
			case "slow":
				// Accept the request and never answer it: the client has to
				// give up on its own deadline.
				<-released
				writeResult(c, req.ID, map[string]any{"late": true})
			case "fast":
				writeResult(c, req.ID, map[string]any{"ok": true})
			}
		})
	})

	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, "slow", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out call = %v, want a deadline error", err)
	}

	if _, err := c.Call(context.Background(), "fast", nil); err != nil {
		t.Fatalf("call after a timeout = %v, want success on a fresh connection", err)
	}

	want := []string{helloVerb, "slow", helloVerb, "fast"}
	if verbs := d.verbs(t, len(want)); !slices.Equal(verbs, want) {
		t.Fatalf("verbs = %v, want %v: the timed-out verb must not be replayed, and the new connection must greet", verbs, want)
	}
	if n := d.connectionCount(); n != 2 {
		t.Fatalf("%d connections, want a second one after the timeout", n)
	}
}

// TestAStaleAnswerIsNotTakenForTheNextCall sends the same answer twice, which
// leaves a line in the reader. The next call must reject that line by id and
// drop the connection rather than hand it to the caller.
func TestAStaleAnswerIsNotTakenForTheNextCall(t *testing.T) {
	d := newSocketDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			switch req.Verb {
			case "a":
				writeResult(c, req.ID, map[string]any{"from": "a"})
				writeResult(c, req.ID, map[string]any{"from": "a"})
			case "b", "c":
				writeResult(c, req.ID, map[string]any{"from": req.Verb})
			}
		})
	})

	c, err := dial(context.Background(), d.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Call(context.Background(), "a", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := c.Call(context.Background(), "b", nil); err == nil || !strings.Contains(err.Error(), "response id") {
		t.Fatalf("call that read the stale line = %v, want an id mismatch", err)
	}
	raw, err := c.Call(context.Background(), "c", nil)
	if err != nil {
		t.Fatalf("call after the stale line = %v, want success on a fresh connection", err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil || got["from"] != "c" {
		t.Fatalf("third call = %s (%v), want the answer to c", raw, err)
	}

	want := []string{helloVerb, "a", "b", helloVerb, "c"}
	if verbs := d.verbs(t, len(want)); !slices.Equal(verbs, want) {
		t.Fatalf("verbs = %v, want %v: nothing replayed, and the new connection greeted", verbs, want)
	}
	if n := d.connectionCount(); n != 2 {
		t.Fatalf("%d connections, want a second one after the stale line", n)
	}
}

// TestAnUndecodableAnswerDiscardsTheConnection: an answer that is not a
// response leaves the connection in an unknown state, since the line that was
// meant to be the answer may still be on its way.
func TestAnUndecodableAnswerDiscardsTheConnection(t *testing.T) {
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			switch req.Verb {
			case "garbage":
				writeLine(c, []byte("<html>\n"))
			default:
				writeResult(c, req.ID, map[string]any{"ok": true})
			}
		})
	})
	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Call(context.Background(), "garbage", nil); err == nil ||
		!strings.Contains(err.Error(), "decode") {
		t.Fatalf("undecodable answer = %v, want a decode error", err)
	}
	if _, err := c.Call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("call after an undecodable answer = %v, want a fresh connection", err)
	}
	want := []string{helloVerb, "garbage", helloVerb, "ping"}
	if verbs := d.verbs(t, len(want)); !slices.Equal(verbs, want) {
		t.Fatalf("verbs = %v, want %v", verbs, want)
	}
	if n := d.connectionCount(); n != 2 {
		t.Fatalf("%d connections, want a second one after the undecodable answer", n)
	}
}

// TestCancelInterruptsABlockedReadWithoutADeadline: a caller that cancels must
// not wait for the daemon, and a call with no deadline of its own must still be
// interruptible.
func TestCancelInterruptsABlockedReadWithoutADeadline(t *testing.T) {
	started := make(chan struct{}, 1)
	released := make(chan struct{})
	defer close(released)
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			switch req.Verb {
			case "hold":
				started <- struct{}{}
				<-released
			default:
				writeResult(c, req.ID, map[string]any{"ok": true})
			}
		})
	})

	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(ctx, "hold", nil)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled call = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not interrupt the blocked read")
	}

	// The connection was dropped with the cancellation, so this is a new one.
	if _, err := c.Call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("call after a cancellation = %v", err)
	}
	want := []string{helloVerb, "hold", helloVerb, "ping"}
	if verbs := d.verbs(t, len(want)); !slices.Equal(verbs, want) {
		t.Fatalf("verbs = %v, want %v", verbs, want)
	}
}

// TestCancelWhileWaitingForTheConnection: calls are serialized, and a caller
// waiting for its turn must be released by its own context, not by the call
// ahead of it finishing.
func TestCancelWhileWaitingForTheConnection(t *testing.T) {
	started := make(chan struct{}, 1)
	released := make(chan struct{})
	defer close(released)
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			switch req.Verb {
			case "hold":
				started <- struct{}{}
				<-released
			default:
				writeResult(c, req.ID, map[string]any{"ok": true})
			}
		})
	})

	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	firstCtx, firstCancel := context.WithCancel(context.Background())
	defer firstCancel()
	firstDone := make(chan error, 1)
	go func() {
		_, err := c.Call(firstCtx, "hold", nil)
		firstDone <- err
	}()
	<-started

	t.Run("deadline", func(t *testing.T) {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := c.Call(ctx, "never", nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("call waiting for the slot = %v, want a deadline error", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("the waiting call took %s, want about its 50ms deadline", elapsed)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		start := time.Now()
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel()
		}()
		if _, err := c.Call(ctx, "never", nil); !errors.Is(err, context.Canceled) {
			cancel()
			t.Fatalf("call waiting for the slot = %v, want context.Canceled", err)
		}
		cancel()
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("the canceled call took %s", elapsed)
		}
	})

	firstCancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("the call holding the slot = %v, want context.Canceled", err)
	}
	if verbs := d.verbs(t, 2); len(verbs) != 2 {
		t.Fatalf("verbs = %v, want only the two the daemon read; a waiting call reached it", verbs)
	}
}

// TestShortWritesAreCompleted: a net.Conn may accept part of a write and
// report no error, and a client that trusted the first n would send a truncated
// request.
func TestShortWritesAreCompleted(t *testing.T) {
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) { writeResult(c, req.ID, req.Params) })
	})
	shortDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := d.dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &shortWriter{Conn: conn, limit: 3}, nil
	}

	c, err := dial(context.Background(), "fake", shortDial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	text := strings.Repeat("payload-", 32)
	raw, err := c.Call(context.Background(), "echo", map[string]any{"text": text})
	if err != nil {
		t.Fatalf("call over a short-writing connection = %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("echo result %s: %v", raw, err)
	}
	if got["text"] != text {
		t.Fatalf("the daemon read %d bytes of the request, want the whole %d", len(got["text"]), len(text))
	}
	if n := d.connectionCount(); n != 1 {
		t.Fatalf("%d connections, want 1", n)
	}
}

// TestResponseLimitIsEnforcedWhileReading: a peer that never sends a newline
// must be cut off at the frame limit instead of being read into a buffer of
// arbitrary size and rejected afterwards. The call has a deadline of its own,
// so a client that waited for the newline would fail this test by timing out
// rather than by naming the limit.
func TestResponseLimitIsEnforcedWhileReading(t *testing.T) {
	old := maxResponseBytes
	maxResponseBytes = 512
	t.Cleanup(func() { maxResponseBytes = old })

	released := make(chan struct{})
	defer close(released)
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			writeLine(c, []byte(strings.Repeat("x", 2048)))
			<-released
		})
	})

	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = c.Call(ctx, "big", nil)
	if err == nil {
		t.Fatal("a response past the frame limit was accepted")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response = %v, want a frame-limit error, not a timeout from waiting for a newline", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("oversized response = %v, want the limit named rather than the call's deadline", err)
	}
}

// TestResponseLimitBoundary proves the limit is inclusive: a response of exactly
// the limit is a response, and one byte more is not.
func TestResponseLimitBoundary(t *testing.T) {
	old := maxResponseBytes
	maxResponseBytes = 512
	t.Cleanup(func() { maxResponseBytes = old })

	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			switch req.Verb {
			case "exact":
				writeLine(c, sizedLine(512, req.ID))
			case "over":
				writeLine(c, sizedLine(513, req.ID))
			}
		})
	})

	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Call(context.Background(), "exact", nil); err != nil {
		t.Fatalf("a response of exactly the limit = %v, want it accepted", err)
	}
	if _, err := c.Call(context.Background(), "over", nil); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("a response one byte over the limit = %v, want a frame-limit error", err)
	}
	// The connection that carried the oversized line is gone.
	if _, err := c.Call(context.Background(), "exact", nil); err != nil {
		t.Fatalf("call after an oversized response = %v, want a fresh connection", err)
	}
	if n := d.connectionCount(); n != 2 {
		t.Fatalf("%d connections, want a second one after the oversized response", n)
	}
}

// TestRequestLimit: a caller's parameter is data, and a request that large must
// be refused before it reaches the daemon.
func TestRequestLimit(t *testing.T) {
	old := maxRequestBytes
	maxRequestBytes = 256
	t.Cleanup(func() { maxRequestBytes = old })

	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) { writeResult(c, req.ID, map[string]any{"ok": true}) })
	})

	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.Call(context.Background(), "big", map[string]any{"text": strings.Repeat("x", 1024)})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized request = %v, want a request-limit error", err)
	}
	if _, err := c.Call(context.Background(), "small", nil); err != nil {
		t.Fatalf("call after an oversized request = %v", err)
	}
	want := []string{helloVerb, "small"}
	if verbs := d.verbs(t, len(want)); !slices.Equal(verbs, want) {
		t.Fatalf("verbs = %v, want %v: the oversized request was never sent, and the connection survived", verbs, want)
	}
	if n := d.connectionCount(); n != 1 {
		t.Fatalf("%d connections, want the readable one kept", n)
	}
}

// TestDaemonErrorsKeepTheConnection: an error envelope is a healthy answer, and
// reconnecting for it would cost a handshake per refused verb.
func TestDaemonErrorsKeepTheConnection(t *testing.T) {
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			switch req.Verb {
			case "denied":
				writeDaemonError(c, req.ID, "forbidden", "another session")
			default:
				writeResult(c, req.ID, map[string]any{"ok": true})
			}
		})
	})

	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.Call(context.Background(), "denied", nil)
	var de *Error
	if !errors.As(err, &de) || de.Code != "forbidden" || de.Message != "another session" {
		t.Fatalf("denied call = %v, want the daemon's forbidden error", err)
	}
	if err.Error() != "forbidden: another session" {
		t.Fatalf("Error() = %q, want the code and the message", err.Error())
	}
	if _, err := c.Call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("call after a daemon error = %v", err)
	}
	if n := d.connectionCount(); n != 1 {
		t.Fatalf("%d connections, want a daemon error to keep the connection", n)
	}
	want := []string{helloVerb, "denied", "ping"}
	if verbs := d.verbs(t, len(want)); !slices.Equal(verbs, want) {
		t.Fatalf("verbs = %v, want %v", verbs, want)
	}
}

func TestUnencodableParamsKeepTheConnection(t *testing.T) {
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) { writeResult(c, req.ID, map[string]any{"ok": true}) })
	})

	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Call(context.Background(), "bad", map[string]any{"f": func() {}}); err == nil ||
		!strings.Contains(err.Error(), "encode") {
		t.Fatalf("unencodable call = %v, want an encode error", err)
	}
	if _, err := c.Call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("call after an encode failure = %v, want the connection kept", err)
	}
	if n := d.connectionCount(); n != 1 {
		t.Fatalf("%d connections, want an encode failure to keep the connection", n)
	}
}

func TestCloseIsIdempotentAndConcurrencySafe(t *testing.T) {
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) { writeResult(c, req.ID, map[string]any{"ok": true}) })
	})
	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), "ping", nil); err != nil {
		t.Fatal(err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("first Close = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- c.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Close = %v", err)
		}
	}

	if _, err := c.Call(context.Background(), "ping", nil); !errors.Is(err, errClientClosed) {
		t.Fatalf("call after Close = %v, want the client-closed error", err)
	}
	if n := d.connectionCount(); n != 1 {
		t.Fatalf("%d connections, want no redial after Close", n)
	}
}

func TestCloseInterruptsACallInFlight(t *testing.T) {
	started := make(chan struct{}, 1)
	released := make(chan struct{})
	defer close(released)
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			started <- struct{}{}
			<-released
			writeResult(c, req.ID, map[string]any{"late": true})
		})
	})
	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "hold", nil)
		done <- err
	}()
	<-started

	start := time.Now()
	if err := c.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errClientClosed) {
			t.Fatalf("call interrupted by Close = %v, want the client-closed error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not interrupt the call in flight")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Close took %s to interrupt the call", elapsed)
	}
	if n := d.connectionCount(); n != 1 {
		t.Fatalf("%d connections, want no redial after Close", n)
	}
}

// TestCallWithoutADeadlineIsBounded: a caller that passes a context with no
// deadline must still not wait forever, and the connection it gives up on must
// not be reused.
func TestCallWithoutADeadlineIsBounded(t *testing.T) {
	old := defaultCallTTL
	defaultCallTTL = 60 * time.Millisecond
	t.Cleanup(func() { defaultCallTTL = old })

	released := make(chan struct{})
	defer close(released)
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) {
			switch req.Verb {
			case "hold":
				<-released
			default:
				writeResult(c, req.ID, map[string]any{"ok": true})
			}
		})
	})

	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.Call(context.Background(), "hold", nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("call with no deadline = %v, want the fallback lifetime to end it", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out call = %v, want an error a caller can test for a deadline", err)
	}
	if _, err := c.Call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("call after the fallback timeout = %v", err)
	}
	if n := d.connectionCount(); n != 2 {
		t.Fatalf("%d connections, want a fresh one after the fallback timeout", n)
	}
}

// TestDialRetryRetriesTransportFailures is the startup case the budget exists
// for: a daemon still binding its socket.
func TestDialRetryRetriesTransportFailures(t *testing.T) {
	cd := &countingDial{fn: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, errors.New("connect: no such file or directory")
	}}
	start := time.Now()
	c, err := dialRetry(context.Background(), "fake", cd.dial, 300*time.Millisecond, 10*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		_ = c.Close()
		t.Fatal("dialRetry returned a client with no daemon")
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("dialRetry gave up after %s, want the whole 300ms budget", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("dialRetry ran for %s, want about 300ms", elapsed)
	}
	if got := cd.count(); got < 3 {
		t.Fatalf("attempts = %d, want several", got)
	}
	if !strings.Contains(err.Error(), "mini: dial fake") {
		t.Fatalf("err = %v, want it to name the socket", err)
	}
}

func TestDialRetryHonorsTheCallersContext(t *testing.T) {
	fail := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, errors.New("connect: no such file or directory")
	}

	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		start := time.Now()
		c, err := dialRetry(ctx, "fake", fail, 5*time.Second, 10*time.Millisecond)
		if err == nil {
			_ = c.Close()
			t.Fatal("dialRetry returned a client with no daemon")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("dialRetry = %v, want the caller's deadline", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("dialRetry ran %s, want the caller's 80ms deadline", elapsed)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(40 * time.Millisecond)
			cancel()
		}()
		start := time.Now()
		c, err := dialRetry(ctx, "fake", fail, 5*time.Second, 10*time.Millisecond)
		if err == nil {
			_ = c.Close()
			t.Fatal("dialRetry returned a client with no daemon")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dialRetry = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("dialRetry ran %s after a cancellation at 40ms", elapsed)
		}
	})
}

// TestDialRetryBudgetIncludesTheHandshake: a daemon that accepts the connection
// and then says nothing must not stretch the wait past the budget, which is why
// the handshake has to be inside it.
func TestDialRetryBudgetIncludesTheHandshake(t *testing.T) {
	released := make(chan struct{})
	defer close(released)
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		d.loop(c, func(req request) { <-released })
	})
	cd := &countingDial{fn: d.dial}

	start := time.Now()
	c, err := dialRetry(context.Background(), "fake", cd.dial, 200*time.Millisecond, 10*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		_ = c.Close()
		t.Fatal("dialRetry returned a client whose hello was never answered")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("the handshake outlived the budget: %s", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("dialRetry = %v, want the budget's deadline", err)
	}
	if got := cd.count(); got != 1 {
		t.Fatalf("attempts = %d, want the first attempt to consume the whole budget", got)
	}
}

// TestDialRetryDoesNotRetryASemanticRefusal: a daemon that answers, and refuses
// the handshake, will answer the same way a moment later.
func TestDialRetryDoesNotRetryASemanticRefusal(t *testing.T) {
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		d.loop(c, func(req request) {
			writeDaemonError(c, req.ID, "protocol_mismatch", "client speaks protocol 9")
		})
	})
	cd := &countingDial{fn: d.dial}

	start := time.Now()
	c, err := dialRetry(context.Background(), "fake", cd.dial, 5*time.Second, 10*time.Millisecond)
	if err == nil {
		_ = c.Close()
		t.Fatal("dialRetry returned a client the daemon refused")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("a semantic refusal was retried for %s", elapsed)
	}
	var de *Error
	if !errors.As(err, &de) || de.Code != "protocol_mismatch" {
		t.Fatalf("dialRetry = %v, want the daemon's protocol_mismatch error", err)
	}
	if got := cd.count(); got != 1 {
		t.Fatalf("attempts = %d, want a refusal answered once", got)
	}
}

func TestDialRetrySucceedsAfterARefusedAttempt(t *testing.T) {
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) { writeResult(c, req.ID, map[string]any{"ok": true}) })
	})
	var attempts int
	fn := func(ctx context.Context, network, addr string) (net.Conn, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("connect: connection refused")
		}
		return d.dial(ctx, network, addr)
	}

	c, err := dialRetry(context.Background(), "fake", fn, 5*time.Second, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("dialRetry = %v, want a client once the daemon is up", err)
	}
	defer c.Close()
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if _, err := c.Call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("call on the retried connection = %v", err)
	}
}

// TestCloseAfterAFailedCallDoesNotRedial closes a client whose connection had
// already been discarded: the next call must not quietly dial again.
func TestCloseAfterAFailedCallDoesNotRedial(t *testing.T) {
	released := make(chan struct{})
	defer close(released)
	d := newPipeDaemon(t, func(d *fakeDaemon, n int, c net.Conn) {
		serveHello(d, c, func(req request) { <-released })
	})
	c, err := dial(context.Background(), "fake", d.dial)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, "slow", nil); err == nil {
		t.Fatal("a call the daemon never answered returned without an error")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if _, err := c.Call(context.Background(), "ping", nil); !errors.Is(err, errClientClosed) {
		t.Fatalf("call after Close = %v, want the client-closed error", err)
	}
	if n := d.connectionCount(); n != 1 {
		t.Fatalf("%d connections, want no redial after Close", n)
	}
}
