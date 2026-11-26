package fly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recorded struct {
	method, uri, auth, contentType string
	body                           map[string]any
}

// fakeFly serves canned responses keyed by "METHOD /path" and records requests.
func fakeFly(t *testing.T, routes map[string]func(w http.ResponseWriter)) (*Client, *[]recorded) {
	t.Helper()
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recorded{method: r.Method, uri: r.URL.RequestURI(), auth: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type")}
		json.NewDecoder(r.Body).Decode(&rec.body)
		reqs = append(reqs, rec)
		if h, ok := routes[r.Method+" "+r.URL.Path]; ok {
			h(w)
			return
		}
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return NewClientWithConfig(srv.URL, "catty-exec", "fly-tok"), &reqs
}

func jsonResp(status int, v any) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	}
}

func TestNewClient_Env(t *testing.T) {
	t.Setenv("FLY_API_TOKEN", "")
	t.Setenv("CATTY_EXEC_APP", "app")
	if _, err := NewClient(); err == nil {
		t.Error("expected error without FLY_API_TOKEN")
	}

	t.Setenv("FLY_API_TOKEN", "tok")
	t.Setenv("CATTY_EXEC_APP", "")
	if _, err := NewClient(); err == nil {
		t.Error("expected error without CATTY_EXEC_APP")
	}

	t.Setenv("CATTY_EXEC_APP", "app")
	t.Setenv("FLY_MACHINES_API_BASE", "")
	c, err := NewClient()
	if err != nil || c.baseURL != DefaultBaseURL || c.AppName() != "app" {
		t.Errorf("got %+v, %v", c, err)
	}

	t.Setenv("FLY_MACHINES_API_BASE", InternalBaseURL)
	if c, _ := NewClient(); c.baseURL != InternalBaseURL {
		t.Errorf("baseURL %q, want override", c.baseURL)
	}
}

func TestCreateMachine(t *testing.T) {
	c, reqs := fakeFly(t, map[string]func(http.ResponseWriter){
		"POST /v1/apps/catty-exec/machines": jsonResp(http.StatusCreated, Machine{ID: "m1", State: "created", Region: "iad"}),
	})
	m, err := c.CreateMachine(&CreateMachineRequest{
		Region: "iad",
		Config: &MachineConfig{Image: "img:1", Env: map[string]string{"CONNECT_TOKEN": "x"}, Guest: &GuestConfig{CPUs: 1, MemoryMB: 1024}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "m1" || m.State != "created" {
		t.Errorf("got %+v", m)
	}
	r := (*reqs)[0]
	if r.auth != "Bearer fly-tok" || r.contentType != "application/json" {
		t.Errorf("headers auth=%q type=%q", r.auth, r.contentType)
	}
	cfg := r.body["config"].(map[string]any)
	if cfg["image"] != "img:1" || r.body["region"] != "iad" {
		t.Errorf("body %v", r.body)
	}
}

func TestCreateMachine_Error(t *testing.T) {
	c, _ := fakeFly(t, map[string]func(http.ResponseWriter){
		"POST /v1/apps/catty-exec/machines": func(w http.ResponseWriter) {
			http.Error(w, "capacity", http.StatusUnprocessableEntity)
		},
	})
	_, err := c.CreateMachine(&CreateMachineRequest{Config: &MachineConfig{}})
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "capacity") {
		t.Errorf("err %v, want status and body", err)
	}
}

func TestMachineLifecycle(t *testing.T) {
	ok := func(w http.ResponseWriter) { w.Write([]byte(`{"ok":true}`)) }
	c, reqs := fakeFly(t, map[string]func(http.ResponseWriter){
		"GET /v1/apps/catty-exec/machines/m1":       jsonResp(http.StatusOK, Machine{ID: "m1", State: "started"}),
		"GET /v1/apps/catty-exec/machines/m1/wait":  ok,
		"POST /v1/apps/catty-exec/machines/m1/stop": ok,
		"DELETE /v1/apps/catty-exec/machines/m1":    func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) },
	})

	m, err := c.GetMachine("m1")
	if err != nil || m.State != "started" {
		t.Fatalf("GetMachine = %+v, %v", m, err)
	}
	if err := c.WaitMachine("m1", "started", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := c.StopMachine("m1"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteMachine("m1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetMachine("missing"); err == nil {
		t.Error("expected error for missing machine")
	}

	want := []string{
		"GET /v1/apps/catty-exec/machines/m1",
		"GET /v1/apps/catty-exec/machines/m1/wait?state=started&timeout=30",
		"POST /v1/apps/catty-exec/machines/m1/stop",
		"DELETE /v1/apps/catty-exec/machines/m1?force=true",
	}
	for i, w := range want {
		if got := (*reqs)[i].method + " " + (*reqs)[i].uri; got != w {
			t.Errorf("request %d = %q, want %q", i, got, w)
		}
	}
}

func TestListMachines_MetadataFilter(t *testing.T) {
	c, reqs := fakeFly(t, map[string]func(http.ResponseWriter){
		"GET /v1/apps/catty-exec/machines": jsonResp(http.StatusOK, []Machine{{ID: "a"}, {ID: "b"}}),
	})
	ms, err := c.ListMachines(map[string]string{"session_id": "s 1"})
	if err != nil || len(ms) != 2 {
		t.Fatalf("ListMachines = %v, %v", ms, err)
	}
	if got := (*reqs)[0].uri; got != "/v1/apps/catty-exec/machines?metadata.session_id=s+1" {
		t.Errorf("uri %q", got)
	}
}

func TestGetCurrentImage(t *testing.T) {
	cases := []struct {
		name     string
		machines []Machine
		want     string
	}{
		{"prefers app process group", []Machine{
			{ID: "x", Config: &MachineConfig{Image: "session-img"}},
			{ID: "y", Config: &MachineConfig{Image: "deploy-img", Metadata: map[string]string{"fly_process_group": "app"}}},
		}, "deploy-img"},
		{"falls back to any image", []Machine{
			{ID: "x"},
			{ID: "y", Config: &MachineConfig{Image: "any-img"}},
		}, "any-img"},
		{"none", []Machine{{ID: "x"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := fakeFly(t, map[string]func(http.ResponseWriter){
				"GET /v1/apps/catty-exec/machines": jsonResp(http.StatusOK, tc.machines),
			})
			got, err := c.GetCurrentImage()
			if tc.want == "" {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
