package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCLI(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sbk_test" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"unauthorized"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch r.Method + " " + r.URL.RequestURI() {
		case "GET /api/v1/status":
			io.WriteString(w, `{"status":"ok"}`)
		case "GET /api/v1/projects/p1/apps?search=web":
			io.WriteString(w, `{"items":[]}`)
		case "POST /api/v1/apps/a1/scale":
			if string(body) != `{"replicas":2}` || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("scale body %q", body)
			}
			w.WriteHeader(204)
		case "POST /api/v1/apps/a1/deploy":
			if string(body) != `{"force_build":true}` {
				t.Errorf("deploy body %q", body)
			}
			w.WriteHeader(202)
			io.WriteString(w, `{"id":"d1","status":"queued"}`)
		case "GET /api/v1/deployments/d1":
			if polls.Add(1) < 2 {
				io.WriteString(w, `{"id":"d1","status":"building"}`)
			} else {
				io.WriteString(w, `{"id":"d1","status":"failed"}`)
			}
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"error":"not found"}`)
		}
	}))
	defer srv.Close()
	t.Setenv("SAILBOX_URL", srv.URL+"/")
	t.Setenv("SAILBOX_API_KEY", "sbk_test")

	call := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := run(args, strings.NewReader(""), &out)
		return out.String(), err
	}

	if out, err := call("status"); err != nil || !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("status: %q %v", out, err)
	}
	if _, err := call("apps", "--project", "p1", "--search", "web"); err != nil {
		t.Fatalf("apps: %v", err)
	}
	if _, err := call("api", "post", "apps/a1/scale", "-d", `{"replicas":2}`); err != nil {
		t.Fatalf("api: %v", err)
	}
	if out, err := call("app", "missing"); err == nil || !strings.Contains(err.Error(), "HTTP 404") || !strings.Contains(out, "not found") {
		t.Fatalf("404 should error and still print body: %q %v", out, err)
	}
	if _, err := call("app"); err != errUsage {
		t.Fatalf("missing arg should be usage error, got %v", err)
	}

	// Flags after the positional arg; --wait polls d1 until it fails.
	pollEvery = time.Millisecond
	if out, err := call("deploy", "a1", "--force-build", "--wait"); err == nil || !strings.Contains(err.Error(), "d1 failed") || !strings.Contains(out, `"failed"`) {
		t.Fatalf("deploy --wait: %q %v", out, err)
	}
	if polls.Load() != 2 {
		t.Fatalf("polled %d times, want 2", polls.Load())
	}

	t.Setenv("SAILBOX_API_KEY", "sbk_wrong")
	if _, err := call("status"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("bad key: %v", err)
	}
}

func TestPlainHTTPGuard(t *testing.T) {
	for host, ok := range map[string]bool{
		"http://127.0.0.1:8080":       true,
		"http://100.107.46.44":        true,
		"http://192.168.1.5":          true,
		"http://box.tail1234.ts.net":  true,
		"http://8.8.8.8":              false,
		"https://sailbox.example.com": true,
	} {
		if _, err := newClient(host, "k", false); (err == nil) != ok {
			t.Errorf("%s: err=%v, want allowed=%v", host, err, ok)
		}
	}
	if _, err := newClient("http://8.8.8.8", "k", true); err != nil {
		t.Errorf("SAILBOX_ALLOW_HTTP should allow: %v", err)
	}
}
