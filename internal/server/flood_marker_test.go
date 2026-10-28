package server

import "testing"

// A Read may split the APC introducer at either byte boundary. Missing this
// produces "graphics=false text=true" even when the stream contains graphics.
func TestFloodGraphicsMarkerAcrossReads(t *testing.T) {
	for _, chunks := range [][][]byte{
		{[]byte("text\x1b"), []byte("_Gframe")},
		{[]byte("text\x1b_"), []byte("Gframe")},
		{[]byte("\x1b"), []byte("\x1b"), []byte("_"), []byte("G")},
	} {
		matched := 0
		seen := false
		for _, chunk := range chunks {
			seen = floodGraphicsMarker(chunk, &matched) || seen
		}
		if !seen {
			t.Fatalf("split graphics introducer was lost: %q", chunks)
		}
	}
}
