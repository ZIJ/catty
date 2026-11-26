package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	sig := "SIGTERM"
	msgs := []any{
		NewResizeMessage(120, 40),
		NewSignalMessage("SIGINT"),
		NewPingMessage(),
		NewPongMessage(),
		NewReadyMessage(),
		NewExitMessage(0, nil),
		NewExitMessage(137, &sig),
		NewErrorMessage("boom"),
	}
	for _, want := range msgs {
		data, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ParseMessage(data)
		if err != nil {
			t.Fatalf("ParseMessage(%s): %v", data, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ParseMessage(%s) = %#v, want %#v", data, got, want)
		}
	}
}

func TestWireFormat(t *testing.T) {
	// The CLI and executor are released separately, so the JSON shape is a contract.
	cases := map[string]any{
		`{"type":"resize","cols":80,"rows":24}`:  NewResizeMessage(80, 24),
		`{"type":"exit","code":1,"signal":null}`: NewExitMessage(1, nil),
		`{"type":"error","message":"x"}`:         NewErrorMessage("x"),
		`{"type":"signal","name":"SIGHUP"}`:      NewSignalMessage("SIGHUP"),
	}
	for want, msg := range cases {
		data, _ := json.Marshal(msg)
		if string(data) != want {
			t.Errorf("got %s, want %s", data, want)
		}
	}
}

func TestParseMessage_UnknownAndInvalid(t *testing.T) {
	got, err := ParseMessage([]byte(`{"type":"future-thing","x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := got.(*BaseMessage); !ok || b.Type != "future-thing" {
		t.Errorf("unknown type: got %#v", got)
	}

	for _, bad := range []string{``, `not json`, `{"type":"resize","cols":-1}`, `{"type":"exit","code":"x"}`} {
		if _, err := ParseMessage([]byte(bad)); err == nil {
			t.Errorf("ParseMessage(%q): expected error", bad)
		}
	}
}
