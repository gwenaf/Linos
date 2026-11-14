package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gwenaf/linos/internal/game"
)

const adminToken = "0123456789abcdef0123456789abcdef"

func TestAdminLogin(t *testing.T) {
	h := New(game.NewRoom(t.TempDir()), web, Options{AdminToken: adminToken})
	do := func(method, path, cookie, origin string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.RemoteAddr, req.Host = phone, "linos.example.org"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-Proto", "https")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: adminCookie, Value: cookie})
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("GET", "/login", "", "", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="token"`) {
		t.Fatalf("login page: %d %q", rec.Code, rec.Body)
	}
	if rec := do("POST", "/login", "", "", url.Values{"token": {"guess"}}); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	rec := do("POST", "/login", "", "", url.Values{"token": {adminToken}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/control" {
		t.Fatalf("right token: %d %v", rec.Code, rec.Header())
	}
	cookies := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		cookies[c.Name] = c
	}
	admin, hint := cookies[adminCookie], cookies[hostCookie]
	if admin == nil || admin.Value != adminToken || !admin.HttpOnly || !admin.Secure || admin.SameSite != http.SameSiteStrictMode {
		t.Fatalf("admin cookie = %+v", admin)
	}
	if hint == nil || hint.HttpOnly || hint.Value != "1" {
		t.Fatalf("host hint cookie = %+v", hint)
	}

	for _, tc := range []struct {
		name, path, cookie, origin string
		want                       int
	}{
		{"signed in", "/api/join", adminToken, "", http.StatusOK},
		{"same site over https", "/api/join", adminToken, "https://linos.example.org", http.StatusOK},
		{"other site", "/api/join", adminToken, "https://evil.example", http.StatusForbidden},
		{"broken origin", "/api/join", adminToken, "%", http.StatusForbidden},
		{"wrong cookie", "/api/join", "guess", "", http.StatusForbidden},
		{"no cookie", "/api/join", "", "", http.StatusForbidden},
		{"control role", "/ws?role=control", adminToken, "", http.StatusUpgradeRequired},
		{"control role without cookie", "/ws?role=control", "", "", http.StatusForbidden},
		{"home", "/", adminToken, "", http.StatusFound},
	} {
		if rec := do("GET", tc.path, tc.cookie, tc.origin, nil); rec.Code != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
	if loc := do("GET", "/", adminToken, "", nil).Header().Get("Location"); loc != "/control" {
		t.Errorf("signed-in home goes to %s, want /control", loc)
	}
}

// Without a token there is no way in from another machine, whatever the cookie says.
func TestNoAdminToken(t *testing.T) {
	h := New(game.NewRoom(t.TempDir()), web, Options{})
	for _, path := range []string{"/login", "/api/join"} {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr, req.Host = phone, "linos.example.org"
		req.AddCookie(&http.Cookie{Name: adminCookie, Value: ""})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want refused", path, rec.Code)
		}
	}
}
