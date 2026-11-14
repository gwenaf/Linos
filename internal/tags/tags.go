package tags

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dhowden/tag"

	"github.com/gwenaf/linos/internal/pack"
)

func isAudio(name string) bool {
	return slices.Contains(pack.AudioExtensions, strings.ToLower(filepath.Ext(name)))
}

// importOutro keeps the music playing a little after the answer, as before tracks had an outro.
const importOutro = 10

// Options shape the generated tracks.
type Options struct {
	Start    float64 // where each extract starts, in seconds
	Duration float64 // extract length, in seconds; 0 keeps the default
	Via      string  // "device" to answer on phones, empty for oral answers
}

type Result struct {
	Pack    string   `json:"pack"`
	Tracks  int      `json:"tracks"`
	Skipped []string `json:"skipped"`
}

// Import saves the uploaded audio files into a new folder pack under packsDir and writes its manifest
// from the files' tags. The folder is removed if nothing playable was uploaded.
func Import(packsDir string, files *multipart.Reader, opts Options) (Result, error) {
	now := time.Now()
	dir, name, err := newPackDir(packsDir, "import-"+now.Format("20060102-150405"))
	if err != nil {
		return Result{}, err
	}
	res := Result{Pack: name}
	var saved Saved
	saved, err = Save(dir, files, isAudio)
	res.Skipped = saved.Skipped
	if err == nil {
		res.Tracks, err = writeManifest(dir, "Import du "+now.Format("02/01/2006 15:04"), opts)
	}
	if err != nil {
		os.RemoveAll(dir)
		return Result{}, err
	}
	return res, nil
}

func newPackDir(packsDir, base string) (dir, name string, err error) {
	if err := os.MkdirAll(packsDir, 0o755); err != nil {
		return "", "", err
	}
	for i := 1; ; i++ {
		name = base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		dir = filepath.Join(packsDir, name)
		if err = os.Mkdir(dir, 0o755); !errors.Is(err, fs.ErrExist) {
			return dir, name, err
		}
	}
}

// Saved is the outcome of an upload: the files written, the files already there (an identical file
// exists: its name, relative to the folder, is returned and nothing is written) and the files refused.
type Saved struct {
	New      []string
	Existing []string
	Skipped  []string
}

// Save writes the uploaded files accept keeps into dir, renaming on conflict. A file identical to one
// already in dir, or below, is not copied again. A file interrupted mid-upload is removed.
func Save(dir string, files *multipart.Reader, accept func(string) bool) (Saved, error) {
	s := Saved{New: []string{}, Existing: []string{}, Skipped: []string{}}
	for {
		part, err := files.NextPart()
		if err == io.EOF {
			return s, nil
		}
		if err != nil {
			return s, err
		}
		name := safeName(part.FileName())
		if !accept(name) {
			if part.FileName() != "" {
				s.Skipped = append(s.Skipped, part.FileName())
			}
			continue
		}
		p := uniquePath(dir, name)
		h := sha256.New()
		f, err := os.Create(p)
		if err == nil {
			_, err = io.Copy(io.MultiWriter(f, h), part)
			err = errors.Join(err, f.Close())
		}
		if err != nil {
			os.Remove(p) // no half-written file left behind
			return s, err
		}
		if same := sameFile(dir, p, h.Sum(nil), accept); same != "" {
			os.Remove(p)
			s.Existing = append(s.Existing, same)
			continue
		}
		s.New = append(s.New, filepath.Base(p))
	}
}

// sameFile finds a file of dir, or below, that accept keeps and that holds the content hashed as sum,
// other than path itself. Only files of the same size are read.
func sameFile(dir, path string, sum []byte, accept func(string) bool) string {
	info, _ := os.Stat(path) // just written
	found := ""
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if found != "" || err != nil || d.IsDir() || p == path || !accept(p) {
			return nil
		}
		i, _ := d.Info()
		f, err := os.Open(p)
		if i != nil && i.Size() == info.Size() && err == nil {
			h := sha256.New()
			if _, err := io.Copy(h, f); err == nil && bytes.Equal(h.Sum(nil), sum) {
				found, _ = filepath.Rel(dir, p) // p is below dir
				found = filepath.ToSlash(found)
			}
		}
		f.Close() // nil-safe
		return nil
	})
	return found
}

// safeName keeps the base name of an uploaded file (browsers may send "folder/song.mp3")
// and replaces the characters Windows or the pack paths refuse.
func safeName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	return strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
}

func uniquePath(dir, name string) string {
	ext := filepath.Ext(name)
	p := filepath.Join(dir, name)
	for i := 2; fileExists(p); i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, ext), i, ext))
	}
	return p
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

type info struct {
	title, artist string
}

// read takes title and artist from the tags, falling back on an "Artist - Title" file name.
func read(p string) info {
	i := fromName(filepath.Base(p))
	f, err := os.Open(p)
	if err != nil {
		return i
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return i
	}
	if t := strings.TrimSpace(m.Title()); t != "" {
		i.title = t
	}
	if a := strings.TrimSpace(m.Artist()); a != "" {
		i.artist = a
	}
	return i
}

// Guesses proposes the artist and title to guess from a file's tags or name.
func Guesses(path string) []pack.Guess {
	i := read(path)
	var guesses []pack.Guess
	if i.artist != "" {
		guesses = append(guesses, pack.Guess{Label: "Artiste", Type: "text", Answers: answers(i.artist)})
	}
	return append(guesses, pack.Guess{Label: "Titre", Type: "text", Answers: answers(i.title)})
}

func fromName(name string) info {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	// Drop a leading track number: "01 - Title", "07. Title".
	if trimmed := strings.TrimLeft(strings.TrimLeft(base, "0123456789"), " .-_"); trimmed != "" {
		base = trimmed
	}
	if artist, title, ok := strings.Cut(base, " - "); ok {
		return info{title: strings.TrimSpace(title), artist: strings.TrimSpace(artist)}
	}
	return info{title: strings.TrimSpace(base)}
}

// answers accepts the tag as written and without its extras: "Song (Remastered 2011)" also accepts "Song".
func answers(s string) []string {
	out := []string{s}
	short := s
	for _, sep := range []string{" (", " [", " - ", " feat"} {
		if i := strings.Index(strings.ToLower(short), sep); i > 0 {
			short = short[:i]
		}
	}
	if short = strings.TrimSpace(short); short != s {
		out = append(out, short)
	}
	return out
}

func writeManifest(dir, title string, opts Options) (int, error) {
	entries, _ := os.ReadDir(dir) // dir was just created and filled
	m := pack.Manifest{Version: pack.SupportedVersion, Title: title, Rules: &pack.Rules{}}
	if opts.Via != "" {
		m.Rules.Answer = &pack.AnswerRules{Via: &opts.Via}
	}
	if opts.Duration > 0 {
		m.Rules.Duration = &opts.Duration
	}
	for _, e := range entries {
		m.Tracks = append(m.Tracks, pack.Track{
			ID:      fmt.Sprintf("t%d", len(m.Tracks)+1),
			Media:   e.Name(),
			Start:   opts.Start,
			Outro:   importOutro,
			Guesses: Guesses(filepath.Join(dir, e.Name())),
		})
	}
	if len(m.Tracks) == 0 {
		return 0, errors.New("no audio file to import (mp3, m4a, aac, ogg, opus, flac, wav)")
	}
	data, _ := json.MarshalIndent(m, "", "  ") // plain structs always marshal
	return len(m.Tracks), os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644)
}
