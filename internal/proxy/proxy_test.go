package proxy

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/izalutski/catty/internal/db"
)

type usage struct {
	userID, sessionID string
	input, output     int64
}

type fakeStore struct {
	mu        sync.Mutex
	sessions  map[string]*db.Session
	allowed   bool
	quotaErr  error
	recorded  []usage
	recordErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		sessions: map[string]*db.Session{
			"brave-tiger-1234": {ID: "sess-1", UserID: "user-1", Label: "brave-tiger-1234"},
		},
		allowed: true,
	}
}

func (f *fakeStore) GetSessionByLabelAnyUser(label string) (*db.Session, error) {
	if s, ok := f.sessions[label]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeStore) CheckQuota(userID string) (bool, int64, error) {
	return f.allowed, 1000, f.quotaErr
}

func (f *fakeStore) RecordUsage(userID, sessionID string, in, out int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, usage{userID, sessionID, in, out})
	return f.recordErr
}

func (f *fakeStore) usages() []usage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]usage(nil), f.recorded...)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// setup starts a fake Anthropic upstream and a proxy in front of it.
func setup(t *testing.T, upstream http.HandlerFunc) (*fakeStore, *httptest.Server) {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)

	st := newFakeStore()
	p, err := newProxy(st, "sk-server-key", up.URL, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return st, srv
}

func post(t *testing.T, url string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"model":"x"}`))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	// Use a transport that does not add Accept-Encoding on its own, so tests
	// control exactly what the proxy receives.
	resp, err := (&http.Transport{DisableCompression: true}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestServeHTTP_RejectsBadPaths(t *testing.T) {
	_, srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called")
	})
	for _, path := range []string{"/v1/messages", "/s/only-label"} {
		if got := post(t, srv.URL+path, nil).StatusCode; got != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", path, got)
		}
	}
}

func TestServeHTTP_UnknownSession(t *testing.T) {
	_, srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called")
	})
	if got := post(t, srv.URL+"/s/no-such-label/v1/messages", nil).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", got)
	}
}

func TestServeHTTP_QuotaExceeded(t *testing.T) {
	st, srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called")
	})
	st.allowed = false
	if got := post(t, srv.URL+"/s/brave-tiger-1234/v1/messages", nil).StatusCode; got != http.StatusPaymentRequired {
		t.Errorf("status %d, want 402", got)
	}
}

func TestServeHTTP_QuotaError(t *testing.T) {
	st, srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called")
	})
	st.quotaErr = errors.New("db down")
	if got := post(t, srv.URL+"/s/brave-tiger-1234/v1/messages", nil).StatusCode; got != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", got)
	}
}

func TestServeHTTP_RewritesRequest(t *testing.T) {
	var gotPath, gotKey, gotAuth string
	_, srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	})
	post(t, srv.URL+"/s/brave-tiger-1234/v1/messages", map[string]string{
		"x-api-key":     "client-supplied",
		"Authorization": "Bearer user-token",
	})
	if gotPath != "/v1/messages" {
		t.Errorf("upstream path %q, want /v1/messages", gotPath)
	}
	if gotKey != "sk-server-key" {
		t.Errorf("upstream x-api-key %q, want server key", gotKey)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header leaked upstream: %q", gotAuth)
	}
}

const jsonBody = `{"id":"msg_1","usage":{"input_tokens":12,"output_tokens":34}}`

func TestNonStreaming_RecordsUsage(t *testing.T) {
	st, srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, jsonBody)
	})
	resp := post(t, srv.URL+"/s/brave-tiger-1234/v1/messages", nil)
	body, _ := io.ReadAll(resp.Body)
	if string(body) != jsonBody {
		t.Errorf("body altered: %s", body)
	}
	want := []usage{{"user-1", "sess-1", 12, 34}}
	if got := st.usages(); len(got) != 1 || got[0] != want[0] {
		t.Errorf("recorded %+v, want %+v", got, want)
	}
}

func TestNonStreaming_GzipClientStillMetered(t *testing.T) {
	st, srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			io.WriteString(gz, jsonBody)
			gz.Close()
			return
		}
		io.WriteString(w, jsonBody)
	})
	resp := post(t, srv.URL+"/s/brave-tiger-1234/v1/messages", map[string]string{"Accept-Encoding": "gzip"})
	io.Copy(io.Discard, resp.Body)
	if got := st.usages(); len(got) != 1 || got[0].input != 12 || got[0].output != 34 {
		t.Errorf("recorded %+v, want one record of 12/34", got)
	}
}

func TestNonStreaming_IgnoresErrorsAndOtherPaths(t *testing.T) {
	st, srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/messages" {
			w.WriteHeader(http.StatusBadRequest)
		}
		io.WriteString(w, jsonBody)
	})
	post(t, srv.URL+"/s/brave-tiger-1234/v1/messages", nil)
	post(t, srv.URL+"/s/brave-tiger-1234/v1/models", nil)
	if got := st.usages(); len(got) != 0 {
		t.Errorf("recorded %+v, want nothing", got)
	}
}

// sseStream builds a typical Messages API stream using nl as the line ending.
func sseStream(nl string) string {
	events := [][2]string{
		{"message_start", `{"type":"message_start","message":{"usage":{"input_tokens":100,"output_tokens":1}}}`},
		{"content_block_delta", `{"type":"content_block_delta","delta":{"text":"hi"}}`},
		{"message_delta", `{"type":"message_delta","usage":{"output_tokens":7}}`},
		{"message_delta", `{"type":"message_delta","usage":{"output_tokens":42}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}
	var b strings.Builder
	for _, e := range events {
		b.WriteString("event: " + e[0] + nl + "data: " + e[1] + nl + nl)
	}
	return b.String()
}

func TestStreaming_RecordsUsageOnce(t *testing.T) {
	for name, sep := range map[string]string{"LF": "\n", "CRLF": "\r\n"} {
		t.Run(name, func(t *testing.T) {
			stream := sseStream(sep)
			st, srv := setup(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				// Write in small chunks so events straddle reads.
				fl := w.(http.Flusher)
				for i := 0; i < len(stream); i += 17 {
					io.WriteString(w, stream[i:min(i+17, len(stream))])
					fl.Flush()
				}
			})
			resp := post(t, srv.URL+"/s/brave-tiger-1234/v1/messages", nil)
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if string(body) != stream {
				t.Errorf("stream altered")
			}
			got := st.usages()
			if len(got) != 1 || got[0] != (usage{"user-1", "sess-1", 100, 42}) {
				t.Errorf("recorded %+v, want one record of 100/42", got)
			}
		})
	}
}

func TestSSEReader_CloseRecordsPartialStream(t *testing.T) {
	st := newFakeStore()
	p := &Proxy{db: st, logger: quietLogger()}
	r := &sseUsageReader{
		reader:  io.NopCloser(strings.NewReader(`data: {"type":"message_start","message":{"usage":{"input_tokens":5}}}` + "\n\n")),
		proxy:   p,
		session: st.sessions["brave-tiger-1234"],
	}
	buf := make([]byte, 4096)
	r.Read(buf) // read the event but stop before EOF, as an aborted client would
	r.Close()
	r.Close()
	if got := st.usages(); len(got) != 1 || got[0].input != 5 {
		t.Errorf("recorded %+v, want one record with 5 input tokens", got)
	}
}

func TestSSEReader_IgnoresGarbage(t *testing.T) {
	st := newFakeStore()
	r := &sseUsageReader{proxy: &Proxy{db: st, logger: quietLogger()}, session: st.sessions["brave-tiger-1234"]}
	r.buffer = []byte("data: not json\n\ndata: [DONE]\n\n: comment\n\npartial")
	r.parseSSEEvents()
	if r.inputTokens != 0 || r.outputTokens != 0 {
		t.Errorf("tokens %d/%d, want 0/0", r.inputTokens, r.outputTokens)
	}
	if !bytes.Equal(r.buffer, []byte("partial")) {
		t.Errorf("buffer %q, want trailing partial event kept", r.buffer)
	}
}

func TestSessionContext(t *testing.T) {
	if SessionFromContext(t.Context()) != nil {
		t.Error("empty context should have no session")
	}
	s := &db.Session{ID: "x"}
	if got := SessionFromContext(ContextWithSession(t.Context(), s)); got != s {
		t.Errorf("got %v, want %v", got, s)
	}
}
