// Package vtcontract_test checks the pinned VT library's public protocol
// contract without changing its upstream-owned module or runtime code.
package vtcontract_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"tuios.dev/vt"
)

// Failure inventory: control keys, error replies, echo classification and
// object names must agree; header-only parsing must not decode image data.
// Payload-prefix bookkeeping is not a public control key. Keep this target
// in the fork's root module so the published VT pin remains unchanged.
func FuzzKittyHeader(f *testing.F) {
	for _, s := range []string{
		"a=t,f=24,s=1,v=1,i=41;AA!A",
		"a=q,f=24,s=1,v=1,t=d,i=42;AA!A",
		"a=q,f=24,s=1,v=1,t=d,i=43;AAAA",
		"a=t,q=2,i=44;AA!A",
		"m=0;AA!A",
		"I=5;A",
		"i=3;EINVAL:bad",
		"i=3,a=T;EINVAL:bad",
		"a=T,t=f,i=9;L3RtcC94",
		"a=T,t=s,i=9;!!",
		"a=q;" + strings.Repeat("QUJD", 100) + "=",
		"a=d,d=a",
		";",
		"a=a,i=1,c=2;",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		full, errFull := vt.ParseKittyCommand(data)
		head, errHead := vt.ParseKittyHeader(data)
		if (errFull == nil) != (errHead == nil) || (full == nil) != (head == nil) {
			t.Fatalf("%q: full parse (%v, %v), header parse (%v, %v)", data, full, errFull, head, errHead)
		}
		if full == nil {
			return
		}
		if !bytes.Equal(vt.KittyPayloadErrorResponse(full), vt.KittyPayloadErrorResponse(head)) {
			t.Fatalf("%q: the full parse replies %q, the header parse %q", data,
				vt.KittyPayloadErrorResponse(full), vt.KittyPayloadErrorResponse(head))
		}
		if vt.IsKittyEchoedResponse(full) != vt.IsKittyEchoedResponse(head) {
			t.Fatalf("%q: the two parses disagree on whether it is an echoed reply", data)
		}
		if head.PayloadErr != nil && full.PayloadErr == nil {
			t.Fatalf("%q: the header parse found a payload error the full parse did not: %v", data, head.PayloadErr)
		}
		if head.Data != nil {
			t.Fatalf("%q: the header parse decoded the image data", data)
		}
		namesObject := (full.Medium == vt.KittyMediumSharedMemory || full.Medium == vt.KittyMediumTempFile ||
			full.Medium == vt.KittyMediumFile) && len(full.RawPayload) <= 8192
		if head.FilePath != full.FilePath && (head.FilePath != "" || namesObject) {
			t.Fatalf("%q: the header parse named %q, the full parse %q", data, head.FilePath, full.FilePath)
		}
		if !sameControls(*full, *head) {
			t.Fatalf("%q: control keys differ: full %+v; header %+v", data, full, head)
		}
	})
}

func sameControls(a, b vt.KittyCommand) bool {
	a.Data, a.FilePath, a.RawPayload, a.PayloadErr = nil, "", "", nil
	b.Data, b.FilePath, b.RawPayload, b.PayloadErr = nil, "", "", nil
	// Compare every exported control field, not private payload bookkeeping
	// such as pngHead (nil versus an empty decoded prefix). Private echo/error
	// state is still checked through the public protocol operations above.
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	for i := range av.NumField() {
		if av.Type().Field(i).IsExported() && !reflect.DeepEqual(av.Field(i).Interface(), bv.Field(i).Interface()) {
			return false
		}
	}
	return true
}

// Fence the oracle itself: ignoring private payload state must never ignore a
// public control key. Iterate all exported non-payload fields, including any a
// future VT release adds, and mutate each in turn.
func TestHeaderOracleChecksEveryControl(t *testing.T) {
	base := vt.KittyCommand{}
	typ := reflect.TypeOf(base)
	payload := map[string]bool{"Data": true, "FilePath": true, "RawPayload": true, "PayloadErr": true}
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() || payload[field.Name] {
			continue
		}
		t.Run(field.Name, func(t *testing.T) {
			changed := base
			value := reflect.ValueOf(&changed).Elem().Field(i)
			switch value.Kind() {
			case reflect.Bool:
				value.SetBool(true)
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				value.SetInt(1)
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				value.SetUint(1)
			default:
				t.Fatalf("new control field %s needs a mutation: %v", field.Name, value.Kind())
			}
			if sameControls(base, changed) {
				t.Fatalf("oracle ignored changed control key %s", field.Name)
			}
		})
	}
}
