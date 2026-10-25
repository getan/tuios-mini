//go:build unix

package mini_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/getan/tuios-mini/mini"
	"github.com/getan/tuios-mini/mini/control"
)

func TestMiniDefaultStateSeparatesSocketsInOneDirectory(t *testing.T) {
	dir := shortDir(t)
	a, err := mini.Start(context.Background(), mini.Config{SocketPath: filepath.Join(dir, "a.sock"), Session: "work", Shell: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Stop)
	b, err := mini.Start(context.Background(), mini.Config{SocketPath: filepath.Join(dir, "b.sock"), Session: "work", Shell: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Stop)
	if a.StateDir() == b.StateDir() {
		t.Fatal("two sockets in one directory shared their default state store")
	}
}

func TestMiniStopsWhenItsLastSessionIsDeleted(t *testing.T) {
	dir := shortDir(t)
	srv, err := mini.Start(context.Background(), mini.Config{
		SocketPath: filepath.Join(dir, "d.sock"), Session: "work", Shell: "/bin/sh", StopWhenEmpty: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	c, err := control.Dial(context.Background(), srv.Socket())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Call(context.Background(), "kill-session", map[string]any{"session": "work"})
	select {
	case <-srv.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("server with no sessions did not stop")
	}
}
