package server

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gwenaf/linos/internal/pack"
	"github.com/gwenaf/linos/internal/tags"
)

// editor serves /edit and its API: host machine only, and only folder packs of packsDir are written.
func editor(mux *http.ServeMux, packsDir string, page http.HandlerFunc) {
	mux.HandleFunc("GET /edit", hostOnly(page))

	mux.HandleFunc("GET /api/edit", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		names := []string{}
		entries, _ := os.ReadDir(packsDir) // a missing packs folder simply means no packs
		for _, e := range entries {
			if _, err := os.Stat(filepath.Join(packsDir, e.Name(), "manifest.json")); e.IsDir() && err == nil {
				names = append(names, e.Name())
			}
		}
		writeJSON(w, names)
	}))

	mux.HandleFunc("POST /api/edit", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		var d struct{ Name string }
		json.NewDecoder(r.Body).Decode(&d) // an unreadable body leaves an empty, refused name
		if !validPackName(d.Name) {
			http.Error(w, "invalid pack name", http.StatusBadRequest)
			return
		}
		dir := filepath.Join(packsDir, d.Name)
		os.MkdirAll(packsDir, 0o755) // Mkdir below reports any failure
		if err := os.Mkdir(dir, 0o755); err != nil {
			http.Error(w, "cannot create pack: "+err.Error(), http.StatusConflict)
			return
		}
		m, _ := json.MarshalIndent(pack.Manifest{Version: pack.SupportedVersion, Title: d.Name, Tracks: []pack.Track{}}, "", "  ")
		os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0o644) // the folder was just created
		slog.Info("pack created", "pack", d.Name)
		w.WriteHeader(http.StatusCreated)
	}))

	mux.HandleFunc("GET /api/edit/{pack}", hostOnly(withPack(packsDir, func(w http.ResponseWriter, r *http.Request, dir string) {
		manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		media := []string{}
		fs.WalkDir(os.DirFS(dir), ".", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && tags.IsMedia(p) {
				media = append(media, p)
			}
			return nil
		})
		writeJSON(w, map[string]any{"manifest": json.RawMessage(manifest), "media": media, "problems": problems(dir)})
	})))

	mux.HandleFunc("PUT /api/edit/{pack}", hostOnly(withPack(packsDir, func(w http.ResponseWriter, r *http.Request, dir string) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err == nil {
			_, err = pack.Decode(data)
		}
		var out bytes.Buffer
		if err == nil {
			err = json.Indent(&out, data, "", "  ")
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), out.Bytes(), 0o644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("pack saved", "pack", r.PathValue("pack"))
		writeJSON(w, map[string]any{"problems": problems(dir)})
	})))

	mux.HandleFunc("POST /api/edit/{pack}/media", hostOnly(withPack(packsDir, func(w http.ResponseWriter, r *http.Request, dir string) {
		files, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "expected a multipart upload of media files", http.StatusBadRequest)
			return
		}
		saved, skipped, err := tags.Save(dir, files, tags.IsMedia)
		if err != nil {
			slog.Warn("media upload failed", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Each saved file comes with the guesses its tags or name suggest, for a new track.
		type upload struct {
			Media   string       `json:"media"`
			Guesses []pack.Guess `json:"guesses"`
		}
		uploads := []upload{}
		for _, name := range saved {
			uploads = append(uploads, upload{name, tags.Guesses(filepath.Join(dir, name))})
		}
		slog.Info("media uploaded", "pack", r.PathValue("pack"), "files", len(saved), "skipped", len(skipped))
		writeJSON(w, map[string]any{"uploads": uploads, "skipped": skipped})
	})))

	mux.HandleFunc("GET /api/edit/{pack}/media/{name...}", hostOnly(withPack(packsDir, func(w http.ResponseWriter, r *http.Request, dir string) {
		http.ServeFileFS(w, r, os.DirFS(dir), r.PathValue("name"))
	})))
}

// withPack resolves the {pack} path value to an existing folder pack.
func withPack(packsDir string, h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("pack")
		dir := filepath.Join(packsDir, name)
		if info, err := os.Stat(dir); !validPackName(name) || err != nil || !info.IsDir() {
			http.Error(w, "no folder pack named "+name, http.StatusNotFound)
			return
		}
		h(w, r, dir)
	}
}

// validPackName accepts a single folder name, nothing that could leave packsDir.
func validPackName(name string) bool {
	return fs.ValidPath(name) && name != "." && !strings.ContainsAny(name, `/\:*?"<>|`)
}

// problems lists what keeps the pack from being played; the editor saves incomplete packs anyway.
func problems(dir string) []string {
	p, err := pack.Open(dir)
	if err != nil {
		return strings.Split(err.Error(), "\n")
	}
	p.Close()
	return []string{}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
