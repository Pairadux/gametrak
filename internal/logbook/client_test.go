package logbook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSendsTheServiceToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sheets/gaming/version" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("CF-Access-Client-Id") != "id.access" || r.Header.Get("CF-Access-Client-Secret") != "cfast_secret" {
			t.Errorf("service token headers = %q / %q", r.Header.Get("CF-Access-Client-Id"), r.Header.Get("CF-Access-Client-Secret"))
		}
		w.Write([]byte(`{"version":7,"changed_at":"2026-10-18T00:00:00.000Z"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL+"/", "gaming", Credentials{ClientID: "id.access", ClientSecret: "cfast_secret"})
	version, err := client.Version(context.Background())
	if err != nil || version != 7 {
		t.Errorf("Version = %d, %v; want 7", version, err)
	}
}

func TestClientErrors(t *testing.T) {
	cases := []struct {
		name      string
		handler   http.HandlerFunc
		status    int
		code      string
		message   string
		temporary bool
	}{
		{
			name: "Logbook's error shape",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error":{"code":"sheet_not_found","message":"There is no sheet called \"gaming\""}}`))
			},
			status: 404, code: "sheet_not_found", message: "no sheet called",
		},
		{
			name: "Access refusing the token",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte("<html><body>Forbidden</body></html>"))
			},
			status: 403, message: "rejected the service token",
		},
		{
			name: "Access redirecting to its login page",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "https://team.cloudflareaccess.com/login", http.StatusFound)
			},
			status: 302, message: "login page",
		},
		{
			name: "D1's daily limit",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"error":{"code":"d1_daily_limit","message":"limit reached"}}`))
			},
			status: 503, code: "d1_daily_limit", message: "limit reached", temporary: true,
		},
		{
			name: "a page that isn't JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("<html>login</html>"))
			},
			status: 200, message: "not Logbook JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			_, err := NewClient(server.URL, "gaming", Credentials{}).Version(context.Background())
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want an APIError", err)
			}
			if apiErr.Status != tc.status || apiErr.Code != tc.code || !strings.Contains(apiErr.Message, tc.message) {
				t.Errorf("error = %+v, want status %d, code %q, message containing %q", apiErr, tc.status, tc.code, tc.message)
			}
			if Temporary(err) != tc.temporary {
				t.Errorf("Temporary = %v, want %v", Temporary(err), tc.temporary)
			}
		})
	}
}

func TestNetworkErrorsAreTemporary(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	_, err := NewClient(server.URL, "gaming", Credentials{}).Version(context.Background())
	if err == nil || !Temporary(err) {
		t.Errorf("error = %v, want a temporary network error", err)
	}
}
