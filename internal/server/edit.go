package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/gwenaf/linos/internal/pack"
	"github.com/gwenaf/linos/internal/tags"
)

// editor serves /edit and its API: host machine only, and only folder packs of packsDir are written.
func editor(mux *http.ServeMux, packsDir string, page http.HandlerFunc, hostOnly func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /edit", hostOnly(page))

	// Folder packs are edited in place; archives are listed so they can be unpacked into a folder.
	mux.HandleFunc("GET /api/edit", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		folders, archives := []string{}, []string{}
		entries, _ := os.ReadDir(packsDir) // a missing packs folder simply means no packs
		for _, e := range entries {
			if _, err := os.Stat(filepath.Join(packsDir, e.Name(), "manifest.json")); e.IsDir() && err == nil {
				folders = append(folders, e.Name())
			} else if !e.IsDir() && filepath.Ext(e.Name()) == ".linospack" {
				archives = append(archives, e.Name())
			}
		}
		writeJSON(w, map[string][]string{"folders": folders, "archives": archives})
	}))

	// Quick start: audio files become a new folder pack, its guesses read from their tags.
	mux.HandleFunc("POST /api/import", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		files, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "expected a multipart upload of audio files", http.StatusBadRequest)
			return
		}
		q := r.URL.Query()
		start, _ := strconv.ParseFloat(q.Get("start"), 64)       // missing or invalid: 0
		duration, _ := strconv.ParseFloat(q.Get("duration"), 64) // missing or invalid: pack default
		res, err := tags.Import(packsDir, files, tags.Options{Start: max(start, 0), Duration: duration, Via: q.Get("via")})
		if err != nil {
			slog.Warn("import failed", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Info("music imported", "pack", res.Pack, "tracks", res.Tracks, "skipped", len(res.Skipped))
		writeJSON(w, res)
	}))

	// A .linospack received from someone joins the library once it is checked: a pack is only data,
	// media and a manifest, never a page or a program (see pack.IsMedia).
	mux.HandleFunc("POST /api/edit/archives", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		files, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "expected a multipart upload of .linospack files", http.StatusBadRequest)
			return
		}
		os.MkdirAll(packsDir, 0o755) // Save reports any failure
		saved, err := tags.Save(packsDir, files, func(name string) bool { return filepath.Ext(name) == ".linospack" })
		invalid := map[string]string{}
		for _, name := range saved.New {
			p, openErr := pack.Open(filepath.Join(packsDir, name))
			if openErr != nil {
				os.Remove(filepath.Join(packsDir, name))
				invalid[name] = openErr.Error()
				continue
			}
			p.Close()
			slog.Info("pack added", "file", name)
		}
		if err != nil {
			slog.Warn("pack upload failed", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"saved": saved.New, "existing": saved.Existing, "skipped": saved.Skipped, "invalid": invalid})
	}))

	mux.HandleFunc("GET /api/edit/archives/{name}", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !validPackName(name) || filepath.Ext(name) != ".linospack" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		http.ServeFile(w, r, filepath.Join(packsDir, name))
	}))

	mux.HandleFunc("POST /api/edit/unpack", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		var d struct{ Name string }
		json.NewDecoder(r.Body).Decode(&d) // an unreadable body leaves an empty, refused name
		base, isArchive := strings.CutSuffix(d.Name, ".linospack")
		if !validPackName(d.Name) || !isArchive || base == "" {
			http.Error(w, "invalid archive name", http.StatusBadRequest)
			return
		}
		// The folder takes the archive's name, numbered if a folder already has it.
		name := base
		for i := 2; fileExists(filepath.Join(packsDir, name)); i++ {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		if err := pack.Unpack(filepath.Join(packsDir, d.Name), filepath.Join(packsDir, name)); err != nil {
			slog.Warn("unpack failed", "archive", d.Name, "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Info("pack unpacked", "archive", d.Name, "folder", name)
		writeJSON(w, map[string]string{"name": name})
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
			if err == nil && !d.IsDir() && pack.IsMedia(p) {
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
		saved, err := tags.Save(dir, files, pack.IsMedia)
		if err != nil {
			slog.Warn("media upload failed", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Each file comes with the guesses its tags or name suggest, for a new track; a file the pack
		// already had is flagged, so the editor can tell the song is already there.
		type upload struct {
			Media     string       `json:"media"`
			Guesses   []pack.Guess `json:"guesses"`
			Duplicate bool         `json:"duplicate,omitempty"`
		}
		uploads := []upload{}
		for _, name := range saved.New {
			uploads = append(uploads, upload{name, tags.Guesses(filepath.Join(dir, name)), false})
		}
		for _, name := range saved.Existing {
			uploads = append(uploads, upload{name, tags.Guesses(filepath.Join(dir, filepath.FromSlash(name))), true})
		}
		slog.Info("media uploaded", "pack", r.PathValue("pack"), "files", len(saved.New), "existing", len(saved.Existing), "skipped", len(saved.Skipped))
		writeJSON(w, map[string]any{"uploads": uploads, "skipped": saved.Skipped})
	})))

	// ponytail: synchronous, the request lasts as long as ffmpeg; stream progress if big packs make it too long.
	mux.HandleFunc("POST /api/edit/{pack}/export", hostOnly(withPack(packsDir, func(w http.ResponseWriter, r *http.Request, dir string) {
		name := r.PathValue("pack") + ".linospack"
		dest := filepath.Join(packsDir, name)
		cut, err := pack.Export(dir, dest)
		if err != nil {
			slog.Warn("export failed", "pack", name, "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		info, _ := os.Stat(dest) // just written
		slog.Info("pack exported", "file", name, "cut", cut, "bytes", info.Size())
		writeJSON(w, map[string]any{"file": name, "cut": cut, "size": info.Size()})
	})))

	// A web page cannot open a local folder: the server shows the pack, or its export, in the file manager.
	mux.HandleFunc("POST /api/edit/{pack}/reveal", hostOnly(withPack(packsDir, func(w http.ResponseWriter, r *http.Request, dir string) {
		target := dir
		if r.URL.Query().Get("archive") != "" {
			target += ".linospack"
		}
		if err := reveal(target); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))

	mux.HandleFunc("GET /api/edit/{pack}/media/{name...}", hostOnly(withPack(packsDir, func(w http.ResponseWriter, r *http.Request, dir string) {
		if !pack.IsMedia(r.PathValue("name")) {
			http.NotFound(w, r)
			return
		}
		inert(w)
		http.ServeFileFS(w, r, os.DirFS(dir), r.PathValue("name"))
	})))
}

// Start launches a program without waiting for it. Tests replace it: the real one opens a window.
var Start = func(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}

var goos = runtime.GOOS

// reveal opens the file manager on path, selecting it where the platform allows.
func reveal(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	switch goos {
	case "windows":
		return Start("explorer", "/select,"+path)
	case "darwin":
		return Start("open", "-R", path)
	default:
		return Start("xdg-open", filepath.Dir(path))
	}
}

// OpenBrowser shows url in the default browser.
func OpenBrowser(url string) error {
	switch goos {
	case "windows":
		return Start(rundll32(), "url.dll,FileProtocolHandler", url)
	case "darwin":
		return Start("open", url)
	default:
		return Start("xdg-open", url)
	}
}

// rundll32 is given by full path: System32, or even SystemRoot, may be missing from the environment
// Linos inherits.
func rundll32() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "rundll32.exe")
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

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
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
