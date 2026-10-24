package s3

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/shyim/local-s3/auth"
	"github.com/shyim/local-s3/storage"
)

func newCORSHandler(t *testing.T, accounts []auth.Account) *Handler {
	t.Helper()
	dataDir, err := os.MkdirTemp("", "s3-cors-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dataDir) })
	store, err := storage.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureBucket("test-bucket"); err != nil {
		t.Fatal(err)
	}
	return NewHandler(store, accounts)
}

func TestPreflightDoesNotRequireAuthentication(t *testing.T) {
	handler := newCORSHandler(t, []auth.Account{{AccessKeyID: "key", SecretAccessKey: "secret"}})
	for _, target := range []string{"/test-bucket/object", "/test-bucket/object?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=example"} {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, target, nil)
			req.Header.Set("Origin", "https://app.example")
			req.Header.Set("Access-Control-Request-Method", "PUT")
			req.Header.Set("Access-Control-Request-Headers", "authorization, content-type, x-amz-date")
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)

			if resp.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", resp.Code, http.StatusNoContent)
			}
			for header, want := range map[string]string{
				"Access-Control-Allow-Origin":  "https://app.example",
				"Access-Control-Allow-Methods": "GET, HEAD, PUT, DELETE, OPTIONS",
				"Access-Control-Allow-Headers": "authorization, content-type, x-amz-date",
				"Access-Control-Max-Age":       "600",
			} {
				if got := resp.Header().Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
			if got := strings.Join(resp.Header().Values("Vary"), ","); !strings.Contains(got, "Origin") || !strings.Contains(got, "Access-Control-Request-Headers") {
				t.Errorf("Vary = %q, missing relevant headers", got)
			}
		})
	}
}

func TestActualRequestCORSIncludingUnauthorized(t *testing.T) {
	t.Run("successful put", func(t *testing.T) {
		handler := newCORSHandler(t, nil)
		req := httptest.NewRequest(http.MethodPut, "/test-bucket/object", strings.NewReader("payload"))
		req.Header.Set("Origin", "https://app.example")
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
		}
		assertActualCORSHeaders(t, resp, "https://app.example")
	})

	t.Run("unauthorized put", func(t *testing.T) {
		handler := newCORSHandler(t, []auth.Account{{AccessKeyID: "key", SecretAccessKey: "secret"}})
		req := httptest.NewRequest(http.MethodPut, "/test-bucket/object", strings.NewReader("payload"))
		req.Header.Set("Origin", "https://app.example")
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusForbidden)
		}
		assertActualCORSHeaders(t, resp, "https://app.example")
	})
}

func assertActualCORSHeaders(t *testing.T, resp *httptest.ResponseRecorder, origin string) {
	t.Helper()
	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
	if got := resp.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("Vary = %q, want Origin", got)
	}
}

func TestPreflightRejectsMissingOriginAndUnsupportedMethod(t *testing.T) {
	handler := newCORSHandler(t, nil)
	cases := []struct {
		name   string
		origin string
		method string
	}{
		{name: "missing origin", method: "PUT"},
		{name: "unsupported method", origin: "https://app.example", method: "PATCH"},
		{name: "missing bucket", origin: "https://app.example", method: "PUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "/test-bucket/object"
			if tc.name == "missing bucket" {
				target = "/"
			}
			req := httptest.NewRequest(http.MethodOptions, target, nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			req.Header.Set("Access-Control-Request-Method", tc.method)
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)
			if resp.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d", resp.Code, http.StatusMethodNotAllowed)
			}
			if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("unexpected Access-Control-Allow-Origin %q", got)
			}
		})
	}
}
