package executor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/izalutski/catty/internal/protocol"
)

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if strings.HasSuffix(name, "/") {
			h.SetMode(os.ModeDir | 0755)
		} else {
			h.SetMode(0644)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	zw.Close()
	path := filepath.Join(t.TempDir(), "ws.zip")
	os.WriteFile(path, buf.Bytes(), 0644)
	return path
}

func TestExtractZip(t *testing.T) {
	dest := t.TempDir()
	zipPath := writeZip(t, map[string]string{
		"README.md":      "hi",
		"src/":           "",
		"src/main.go":    "package main",
		"deep/a/b/c.txt": "c",
	})
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"README.md": "hi", "src/main.go": "package main", "deep/a/b/c.txt": "c"} {
		got, err := os.ReadFile(filepath.Join(dest, rel))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", rel, got, err, want)
		}
	}
}

func TestExtractZip_RejectsZipSlip(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "ws")
	os.Mkdir(dest, 0755)
	for _, name := range []string{"../evil.txt", "a/../../evil.txt", "../ws-sibling/x"} {
		zipPath := writeZip(t, map[string]string{name: "pwned"})
		if err := extractZip(zipPath, dest); err == nil {
			t.Errorf("%q: expected error", name)
		}
	}
	if _, err := os.Stat(filepath.Join(parent, "evil.txt")); err == nil {
		t.Error("file escaped the destination directory")
	}
}

func TestExtractZip_NotAZip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.zip")
	os.WriteFile(path, []byte("not a zip"), 0644)
	if err := extractZip(path, t.TempDir()); err == nil {
		t.Error("expected error")
	}
}

func TestValidateToken(t *testing.T) {
	s := &Server{connectToken: "secret"}
	cases := map[string]bool{
		"":              false,
		"secret":        false,
		"Bearer secret": true,
		"bearer secret": true,
		"Bearer wrong":  false,
		"Basic secret":  false,
	}
	for hdr, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if hdr != "" {
			r.Header.Set("Authorization", hdr)
		}
		if got := s.validateToken(r); got != want {
			t.Errorf("validateToken(%q) = %v, want %v", hdr, got, want)
		}
	}

	open := &Server{}
	if !open.validateToken(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Error("no configured token should allow all")
	}
}

func TestNewServer_Env(t *testing.T) {
	t.Setenv("CONNECT_TOKEN", "tok")
	t.Setenv("CATTY_CMD", "claude --dangerously-skip-permissions")
	s := NewServer()
	if s.connectToken != "tok" || len(s.cmd) != 2 || s.cmd[0] != "claude" {
		t.Errorf("got token=%q cmd=%v", s.connectToken, s.cmd)
	}

	t.Setenv("CATTY_CMD", "")
	if s := NewServer(); len(s.cmd) != 1 || s.cmd[0] != "/bin/sh" {
		t.Errorf("default cmd %v, want /bin/sh", s.cmd)
	}
}

func TestHandlers(t *testing.T) {
	s := &Server{connectToken: "tok", cmd: []string{"/bin/sh"}}
	h := s.Handler()

	do := func(method, path, auth string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(""))
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	if got := do(http.MethodGet, "/healthz", ""); got != http.StatusOK {
		t.Errorf("healthz %d", got)
	}
	if got := do(http.MethodGet, "/upload", "Bearer tok"); got != http.StatusMethodNotAllowed {
		t.Errorf("GET upload %d, want 405", got)
	}
	if got := do(http.MethodPost, "/upload", "Bearer nope"); got != http.StatusUnauthorized {
		t.Errorf("unauthorized upload %d, want 401", got)
	}
	if got := do(http.MethodGet, "/connect", ""); got != http.StatusUnauthorized {
		t.Errorf("unauthorized connect %d, want 401", got)
	}

	s.workspaceReady = true
	if got := do(http.MethodPost, "/upload", "Bearer tok"); got != http.StatusConflict {
		t.Errorf("second upload %d, want 409", got)
	}
}

// TestConnect_EndToEnd drives a real shell through the WebSocket relay.
func TestConnect_EndToEnd(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	s := &Server{connectToken: "tok", cmd: []string{"/bin/sh"}}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/connect", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer tok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	readControl := func() any {
		t.Helper()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if typ == websocket.MessageText {
				msg, err := protocol.ParseMessage(data)
				if err != nil {
					t.Fatal(err)
				}
				return msg
			}
		}
	}

	if _, ok := readControl().(*protocol.ReadyMessage); !ok {
		t.Fatal("first control message should be ready")
	}

	send := func(typ websocket.MessageType, v any) {
		t.Helper()
		var data []byte
		if b, ok := v.([]byte); ok {
			data = b
		} else {
			data, _ = json.Marshal(v)
		}
		if err := conn.Write(ctx, typ, data); err != nil {
			t.Fatal(err)
		}
	}

	send(websocket.MessageText, protocol.NewResizeMessage(100, 30))
	send(websocket.MessageText, protocol.NewPingMessage())
	if _, ok := readControl().(*protocol.PongMessage); !ok {
		t.Fatal("expected pong after ping")
	}

	send(websocket.MessageBinary, []byte("stty size; echo marker-$((40+2)); exit 3\n"))

	var out bytes.Buffer
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (output so far %q)", err, out.String())
		}
		if typ == websocket.MessageBinary {
			out.Write(data)
			continue
		}
		msg, _ := protocol.ParseMessage(data)
		if exit, ok := msg.(*protocol.ExitMessage); ok {
			if exit.Code != 3 {
				t.Errorf("exit code %d, want 3", exit.Code)
			}
			break
		}
	}
	if !strings.Contains(out.String(), "marker-42") {
		t.Errorf("shell output missing marker: %q", out.String())
	}
	if !strings.Contains(out.String(), "30 100") {
		t.Errorf("resize not applied, output: %q", out.String())
	}
}
