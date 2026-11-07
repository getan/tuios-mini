package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tuios.dev/vt"
)

// The browser can place frames asynchronously, outside the compositor's
// normal placement list. A preview must hide the current frame, suppress new
// frames and restore resident pixels after closing without stealing input from
// the sidebar when the preview is blurred.
func TestDocumentPreviewBlocksLiveBrowserGraphics(t *testing.T) {
	withClientCaps(t, &HostCapabilities{
		KittyGraphics: true, TerminalName: "kitty", CellWidth: 10, CellHeight: 20,
	})
	rec := &recWriter{}
	kp := NewKittyPassthroughWithOptions(KittyPassthroughOptions{Output: rec, RemoteClient: true})
	const winID = "preview-browser"
	t.Cleanup(func() { kp.OnWindowClose(winID) })
	path := filepath.Join(kittyTestTempDir(t), "browser.rgba")
	writeFrame := func(seed byte) {
		t.Helper()
		data := make([]byte, 400*300*4)
		for i := range data {
			data[i] = seed + byte(i)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := &vt.KittyCommand{
		Action: vt.KittyActionTransmitPlace, Medium: vt.KittyMediumFile,
		Format: vt.KittyFormatRGBA, ImageID: 1, Width: 400, Height: 300, FilePath: path,
	}
	raw := []byte("\x1b_Ga=T,t=f,i=1,f=32,s=400,v=300;" + b64(path) + "\x1b\\")
	send := func() { kp.ForwardCommand(cmd, raw, winID, 0, 0, 98, 28, 1, 1, 0, 0, 0, false, nil) }
	writeFrame(0)
	send()
	if !sendUntil(t, send, func() bool { return rec.has("a=T,i=") }, 2*time.Second) {
		t.Fatal("fixture did not start the browser stream")
	}
	m := gitDrawerGraphicsOS(t, kp, winID)
	m.flushGraphicsForView()
	hidden := rec.count("a=d,d=i,i=")
	m.docPreview.open = true
	m.flushGraphicsForView()
	if got := rec.count("a=d,d=i,i="); got != hidden+1 {
		t.Fatalf("document preview stayed behind live browser: deletes %d -> %d", hidden, got)
	}
	if m.InputOverlayOpen() {
		t.Fatal("a blurred document preview became a modal input overlay")
	}
	shown, streamed := rec.count("a=p,i="), rec.count("a=T,i=")
	for range 3 {
		writeFrame(55)
		send()
		m.flushGraphicsForView()
	}
	time.Sleep(50 * time.Millisecond)
	if rec.count("a=T,i=") != streamed || rec.count("a=p,i=") != shown {
		t.Fatal("a live browser frame painted over the document preview")
	}
	m.docPreview.open = false
	m.flushGraphicsForView()
	if rec.count("a=p,i=") != shown+1 {
		t.Fatal("browser did not return immediately after closing the preview")
	}
	writeFrame(200)
	if !sendUntil(t, send, func() bool { return rec.count("a=T,i=") > streamed }, 2*time.Second) {
		t.Fatal("browser stream did not resume after closing the preview")
	}
	rec.mu.Lock()
	protocol := rec.buf.String()
	rec.mu.Unlock()
	if strings.Contains(protocol, "a=d,d=I,") {
		t.Fatal("preview destroyed resident browser image data")
	}
}
