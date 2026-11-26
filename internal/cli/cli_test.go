package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestCredentialsRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if IsLoggedIn() || GetAccessToken() != "" {
		t.Fatal("fresh home should not be logged in")
	}
	if err := SaveCredentials(&Credentials{AccessToken: "tok", Email: "a@b.c"}); err != nil {
		t.Fatal(err)
	}
	path, _ := credentialsPath()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Errorf("credentials file mode %v (err %v), want 0600", info.Mode().Perm(), err)
	}
	if !IsLoggedIn() || GetAccessToken() != "tok" {
		t.Error("expected logged in with tok")
	}

	SaveCredentials(&Credentials{AccessToken: "tok", ExpiresAt: time.Now().Add(-time.Minute)})
	if IsLoggedIn() {
		t.Error("expired credentials should not count as logged in")
	}

	if err := DeleteCredentials(); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCredentials(); err != nil {
		t.Errorf("deleting twice: %v", err)
	}
	if IsLoggedIn() {
		t.Error("expected logged out after delete")
	}
}

func TestShouldIgnore(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("# comment\n\n/catty-api\nbuild/\n*.tmp\ndocs/*.pdf\n"), 0644)
	w := NewWorkspaceUploader(dir)

	cases := map[string]bool{
		".":                           false,
		"main.go":                     false,
		"cmd/catty/main.go":           false,
		".git":                        true,
		".git/HEAD":                   true,
		"node_modules/x/index.js":     true,
		"pkg/node_modules/x/index.js": true,
		".env":                        true,
		"app.log":                     true,
		"sub/__pycache__/m.pyc":       true,
		"catty-api":                   true, // root-anchored rule
		"build":                       true,
		"build/out.bin":               true,
		"src/a.tmp":                   true,
		"docs/spec.pdf":               true,
		"docs/spec.md":                false,
	}
	for path, want := range cases {
		if got := w.shouldIgnore(path); got != want {
			t.Errorf("shouldIgnore(%q) = %v, want %v", path, got, want)
		}
	}
}

func zipNames(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func TestCreateZip(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0755)
		os.WriteFile(p, []byte(content), 0644)
	}
	write("main.go", "package main")
	write("pkg/util.go", "package pkg")
	write(".git/HEAD", "ref")
	write("node_modules/dep/index.js", "x")
	write("secret.tmp", "x")
	write(".gitignore", "*.tmp\n")

	data, err := NewWorkspaceUploader(dir).CreateZip()
	if err != nil {
		t.Fatal(err)
	}
	got := zipNames(t, data)
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	want := []string{".gitignore", "main.go", "pkg/", "pkg/util.go"}
	if len(names) != len(want) {
		t.Fatalf("zip entries %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("zip entries %v, want %v", names, want)
		}
	}
	if got["pkg/util.go"] != "package pkg" {
		t.Errorf("pkg/util.go content %q", got["pkg/util.go"])
	}
}

func TestCreateZip_Symlinks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "real.txt"), []byte("hello"), 0644)
	os.Mkdir(filepath.Join(dir, "realdir"), 0755)
	os.WriteFile(filepath.Join(dir, "realdir", "a.txt"), []byte("a"), 0644)
	os.Symlink("real.txt", filepath.Join(dir, "link.txt"))
	os.Symlink("realdir", filepath.Join(dir, "linkdir"))
	os.Symlink("missing", filepath.Join(dir, "dangling"))

	data, err := NewWorkspaceUploader(dir).CreateZip()
	if err != nil {
		t.Fatalf("CreateZip with symlinks: %v", err)
	}
	got := zipNames(t, data)
	if got["link.txt"] != "hello" {
		t.Errorf("file symlink should be followed, got %q", got["link.txt"])
	}
	if got["realdir/a.txt"] != "a" {
		t.Error("real directory contents missing")
	}
}

func TestUpload(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0644)

	var gotAuth, gotInstance, gotType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotInstance = r.Header.Get("fly-force-instance-id")
		gotType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()

	if err := NewWorkspaceUploader(dir).Upload(srv.URL+"/upload", "tok", "m-1"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotInstance != "m-1" || gotType != "application/zip" {
		t.Errorf("headers auth=%q instance=%q type=%q", gotAuth, gotInstance, gotType)
	}
	if zipNames(t, gotBody)["f.txt"] != "x" {
		t.Error("uploaded zip missing f.txt")
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusConflict)
	}))
	defer failing.Close()
	if err := NewWorkspaceUploader(dir).Upload(failing.URL, "tok", "m-1"); err == nil {
		t.Error("expected error on non-200 upload")
	}
}

func TestBuildUploadURL(t *testing.T) {
	cases := map[string]string{
		"wss://exec.catty.dev/connect":      "https://exec.catty.dev/upload",
		"ws://localhost:8080/connect":       "http://localhost:8080/upload",
		"wss://connect.example.com/connect": "https://connect.example.com/upload",
	}
	for in, want := range cases {
		if got := buildUploadURL(in); got != want {
			t.Errorf("buildUploadURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Now()
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{5 * time.Second, "5s ago"},
		{3 * time.Minute, "3m ago"},
		{5 * time.Hour, "5h ago"},
		{50 * time.Hour, "2d ago"},
	}
	for _, c := range cases {
		if got := formatAge(now.Add(-c.ago)); got != c.want {
			t.Errorf("formatAge(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}

func newTestClient(t *testing.T, h http.HandlerFunc) *APIClient {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	SaveCredentials(&Credentials{AccessToken: "user-tok"})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewAPIClient(srv.URL)
}

func TestAPIClient_CreateSession(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer user-tok" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		var req CreateSessionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Agent != "claude" {
			t.Errorf("agent %q", req.Agent)
		}
		json.NewEncoder(w).Encode(CreateSessionResponse{SessionID: "s1", Label: "brave-tiger-1234"})
	})
	resp, err := c.CreateSession(&CreateSessionRequest{Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SessionID != "s1" || resp.Label != "brave-tiger-1234" {
		t.Errorf("got %+v", resp)
	}
}

func TestAPIClient_QuotaExceeded(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"error":"quota_exceeded","message":"Out of tokens","upgrade_url":"https://x/up"}`))
	})
	_, err := c.CreateSession(&CreateSessionRequest{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T %v", err, err)
	}
	if !apiErr.IsQuotaExceeded() || apiErr.UpgradeURL != "https://x/up" || apiErr.Error() != "Out of tokens" {
		t.Errorf("got %+v", apiErr)
	}
}

func TestAPIClient_PlainTextError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := c.ListSessions()
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 500 || apiErr.IsQuotaExceeded() {
		t.Fatalf("got %T %v", err, err)
	}
}

func TestAPIClient_SessionEndpoints(t *testing.T) {
	var calls []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/v1/sessions":
			json.NewEncoder(w).Encode([]SessionInfo{{SessionID: "a"}, {SessionID: "b"}})
		case "/v1/sessions/a":
			json.NewEncoder(w).Encode(SessionInfo{SessionID: "a", MachineState: "started"})
		case "/v1/billing/checkout":
			json.NewEncoder(w).Encode(map[string]string{"checkout_url": "https://pay"})
		default:
			w.Write([]byte(`{"status":"stopped"}`))
		}
	})

	list, err := c.ListSessions()
	if err != nil || len(list) != 2 {
		t.Fatalf("ListSessions = %v, %v", list, err)
	}
	s, err := c.GetSession("a", true)
	if err != nil || s.MachineState != "started" {
		t.Fatalf("GetSession = %+v, %v", s, err)
	}
	if err := c.StopSession("a", true); err != nil {
		t.Fatal(err)
	}
	if u, err := c.CreateCheckoutSession(); err != nil || u != "https://pay" {
		t.Fatalf("CreateCheckoutSession = %q, %v", u, err)
	}

	want := []string{
		"GET /v1/sessions",
		"GET /v1/sessions/a?live=true",
		"POST /v1/sessions/a/stop?delete=true",
		"POST /v1/billing/checkout",
	}
	for i := range want {
		if i >= len(calls) || calls[i] != want[i] {
			t.Fatalf("calls %v, want %v", calls, want)
		}
	}
}
