package server

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/gwenaf/linos/internal/ws"
)

// adminCookie carries the admin token. hostCookie only tells the pages to show the host controls:
// readable by scripts, it grants nothing.
const (
	adminCookie = "linos_admin"
	hostCookie  = "linos_host"
)

// hostRights reports a request with the host machine's rights: from the host machine itself, or from
// a browser signed in with the admin token of a self-hosted server.
func hostRights(token string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		if ws.IsLocal(r) {
			return true
		}
		c, err := r.Cookie(adminCookie)
		return token != "" && err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1
	}
}

const loginPage = `<!doctype html>
<html lang="fr"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Linos</title>
<body style="font-family: monospace; max-width: 32em; margin: 3em auto; padding: 0 1em">
<h1>Linos</h1>
<form method="post" action="/login">
<p><label>Jeton administrateur<br><input type="password" name="token" autocomplete="current-password" autofocus required style="width: 100%"></label></p>
<p><button>Se connecter</button></p>
</form>`

// adminLogin serves /login: the right token sets the admin cookie, valid for this browser only.
func adminLogin(mux *http.ServeMux, token string) {
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(loginPage))
	})
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		given := r.PostFormValue("token")
		if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			slog.Warn("admin login refused", "remote", r.RemoteAddr)
			http.Error(w, "wrong token", http.StatusForbidden)
			return
		}
		// Secure when the browser reached the server over HTTPS, usually through a reverse proxy.
		secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
		const month = 30 * 24 * 3600
		http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: token, Path: "/", MaxAge: month,
			HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
		http.SetCookie(w, &http.Cookie{Name: hostCookie, Value: "1", Path: "/", MaxAge: month,
			Secure: secure, SameSite: http.SameSiteStrictMode})
		slog.Info("admin signed in", "remote", r.RemoteAddr)
		http.Redirect(w, r, "/control", http.StatusSeeOther)
	})
}
