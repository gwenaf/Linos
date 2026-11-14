package server

import (
	"encoding/json"
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

// A self-hosted server lets in only the phones that scanned its QR code, and the host's rights.
func TestJoinKey(t *testing.T) {
	h := New(game.NewRoom(t.TempDir()), web, Options{AdminToken: adminToken, PublicURL: "https://quiz.example"})
	do := func(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr, req.Host = phone, "quiz.example"
		req.Header.Set("X-Forwarded-Proto", "https")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	admin := &http.Cookie{Name: adminCookie, Value: adminToken}

	var join struct{ URLs []string }
	if err := json.NewDecoder(do("/api/join", admin).Body).Decode(&join); err != nil || len(join.URLs) != 1 {
		t.Fatalf("join urls: %v %v", join, err)
	}
	key, ok := strings.CutPrefix(join.URLs[0], "https://quiz.example/play?k=")
	if !ok || len(key) < 20 {
		t.Fatalf("join url %q carries no key", join.URLs[0])
	}

	if c := do("/play?k=guess").Result().Cookies(); len(c) != 0 {
		t.Errorf("wrong key sets %v", c)
	}
	c := do("/play?k=" + key).Result().Cookies()
	if len(c) != 1 || c[0].Name != joinCookie || c[0].Value != key || !c[0].HttpOnly || !c[0].Secure {
		t.Fatalf("join cookie = %+v", c)
	}
	for _, tc := range []struct {
		name   string
		cookie *http.Cookie
		want   int
	}{
		{"scanned", c[0], http.StatusUpgradeRequired},
		{"host rights", admin, http.StatusUpgradeRequired},
		{"no key", nil, http.StatusForbidden},
		{"wrong key", &http.Cookie{Name: joinCookie, Value: "guess"}, http.StatusForbidden},
	} {
		var cookies []*http.Cookie
		if tc.cookie != nil {
			cookies = append(cookies, tc.cookie)
		}
		if rec := do("/ws", cookies...); rec.Code != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, rec.Code, tc.want)
		}
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
