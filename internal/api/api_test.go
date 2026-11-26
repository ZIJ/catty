package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExtractBearerToken(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"Bearer ":       "",
		"Bearer abc":    "abc",
		"Basic abc":     "",
		"Bearerabc":     "",
		"Bearer a b c ": "a b c ",
	}
	for hdr, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", hdr)
		if got := extractBearerToken(r); got != want {
			t.Errorf("extractBearerToken(%q) = %q, want %q", hdr, got, want)
		}
	}
}

func TestUserContext(t *testing.T) {
	if UserFromContext(t.Context()) != nil {
		t.Error("empty context should have no user")
	}
	u := &User{ID: "u1"}
	if got := UserFromContext(WithUser(t.Context(), u)); got != u {
		t.Errorf("got %v", got)
	}
}

// newAuth returns AuthHandlers whose cache already holds tokens, so tests
// never reach WorkOS.
func newAuth(cached map[string]*User) *AuthHandlers {
	h := &AuthHandlers{tokenCache: map[string]*tokenCacheEntry{}}
	for tok, u := range cached {
		h.tokenCache[tok] = &tokenCacheEntry{user: u, expiresAt: time.Now().Add(time.Hour)}
	}
	return h
}

func TestAuthMiddleware(t *testing.T) {
	h := newAuth(map[string]*User{"good": {ID: "u1", Email: "a@b.c"}})
	var seen *User
	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = UserFromContext(r.Context())
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no header: status %d, want 401", w.Code)
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error == "" {
		t.Errorf("error body %q not JSON with error field", w.Body.String())
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer good")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || seen == nil || seen.ID != "u1" {
		t.Errorf("cached token: status %d user %v", w.Code, seen)
	}
}

func TestGenerateToken(t *testing.T) {
	a, err := generateToken(32)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := generateToken(32)
	if a == b {
		t.Error("tokens should differ")
	}
	raw, err := base64.URLEncoding.DecodeString(a)
	if err != nil || len(raw) != 32 {
		t.Errorf("token %q decodes to %d bytes (%v), want 32", a, len(raw), err)
	}
}

func TestJoinCmd(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, "/bin/sh"},
		{[]string{"claude"}, "claude"},
		{[]string{"claude", "--dangerously-skip-permissions"}, "claude --dangerously-skip-permissions"},
	}
	for _, c := range cases {
		if got := joinCmd(c.in); got != c.want {
			t.Errorf("joinCmd(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusCreated, map[string]int{"n": 1})
	if w.Code != http.StatusCreated || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("status %d type %q", w.Code, w.Header().Get("Content-Type"))
	}
	if w.Body.String() != "{\"n\":1}\n" {
		t.Errorf("body %q", w.Body.String())
	}
}
