package tags

import (
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

// audio lists the extensions browsers play natively; an import keeps only these files.
var audio = []string{".mp3", ".m4a", ".aac", ".ogg", ".opus", ".flac", ".wav"}

func isAudio(name string) bool {
	return slices.Contains(audio, strings.ToLower(filepath.Ext(name)))
}

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
	res := Result{Pack: name, Skipped: []string{}}
	err = save(dir, files, &res)
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

func save(dir string, files *multipart.Reader, res *Result) error {
	for {
		part, err := files.NextPart()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := safeName(part.FileName())
		if !isAudio(name) {
			if part.FileName() != "" {
				res.Skipped = append(res.Skipped, part.FileName())
			}
			continue
		}
		f, err := os.Create(uniquePath(dir, name))
		if err == nil {
			_, err = io.Copy(f, part)
			err = errors.Join(err, f.Close())
		}
		if err != nil {
			return err
		}
	}
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
		i := read(filepath.Join(dir, e.Name()))
		var guesses []pack.Guess
		if i.artist != "" {
			guesses = append(guesses, pack.Guess{Label: "Artiste", Type: "text", Answers: answers(i.artist)})
		}
		guesses = append(guesses, pack.Guess{Label: "Titre", Type: "text", Answers: answers(i.title)})
		m.Tracks = append(m.Tracks, pack.Track{
			ID:      fmt.Sprintf("t%d", len(m.Tracks)+1),
			Media:   e.Name(),
			Start:   opts.Start,
			Guesses: guesses,
		})
	}
	if len(m.Tracks) == 0 {
		return 0, errors.New("no audio file to import (mp3, m4a, aac, ogg, opus, flac, wav)")
	}
	data, _ := json.MarshalIndent(m, "", "  ") // plain structs always marshal
	return len(m.Tracks), os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644)
}
