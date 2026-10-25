package session

import (
	"bytes"
	"reflect"
	"testing"
)

// Renaming the module must not rename any registered interface payload, not
// just TerminalState (whose old name was already frozen before the fork).
func TestForkKeepsAllInterfacePayloadNames(t *testing.T) {
	for _, value := range []any{
		HelloPayload{}, WelcomePayload{}, SessionInfo{}, SessionState{},
		WindowState{}, SerializedBSPTree{}, CommandResultPayload{},
	} {
		name := reflect.TypeOf(value).Name()
		t.Run(name, func(t *testing.T) {
			data, err := encodePayload(map[string]any{"value": value})
			if err != nil {
				t.Fatal(err)
			}
			want := "github.com/Gaurav-Gosain/tuios/internal/session." + name
			if !bytes.Contains(data, []byte(want)) {
				t.Fatalf("interface payload omitted the legacy name %q", want)
			}
			if bytes.Contains(data, []byte("github.com/getan/tuios-mini/internal/session.")) {
				t.Fatal("the fork's source module path leaked into the wire format")
			}
		})
	}
}
