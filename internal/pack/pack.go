package pack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// MaxManifest bounds manifest.json: a compressed one could otherwise expand without limit.
const MaxManifest = 1 << 20

// AudioExtensions and mediaExtensions list what browsers play or show natively. A pack may only use
// these: served from the host, an .html or .svg file would run scripts with the host's rights.
var (
	AudioExtensions = []string{".mp3", ".m4a", ".aac", ".ogg", ".opus", ".flac", ".wav"}
	mediaExtensions = append([]string{".mp4", ".m4v", ".webm", ".jpg", ".jpeg", ".png", ".webp", ".gif"}, AudioExtensions...)
)

// IsMedia reports a file a pack can use: audio, video or image.
func IsMedia(name string) bool {
	return slices.Contains(mediaExtensions, strings.ToLower(filepath.Ext(name)))
}

// Pack is a gamepack opened from a folder or a .linospack zip archive.
type Pack struct {
	Manifest Manifest

	fsys fs.FS
	// file and zipFiles are only set for zip archives.
	file     *os.File
	zipFiles map[string]*zip.File
}

func Open(path string) (*Pack, error) {
	info, statErr := os.Stat(path)
	f, openErr := os.Open(path)
	if err := errors.Join(statErr, openErr); err != nil {
		f.Close() // nil-safe
		return nil, err
	}

	p := &Pack{}
	if info.IsDir() {
		f.Close()
		p.fsys = os.DirFS(path)
	} else {
		zr, err := zip.NewReader(f, info.Size())
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("%s is neither a folder nor a zip archive: %w", path, err)
		}
		p.file, p.fsys, p.zipFiles = f, zr, map[string]*zip.File{}
		for _, zf := range zr.File {
			p.zipFiles[zf.Name] = zf
		}
	}

	if err := p.load(); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func (p *Pack) Close() error {
	if p.file == nil {
		return nil
	}
	return p.file.Close()
}

func (p *Pack) load() error {
	f, err := p.fsys.Open("manifest.json")
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxManifest+1))
	f.Close()
	if err == nil && len(data) > MaxManifest {
		err = fmt.Errorf("manifest.json is larger than %d bytes", MaxManifest)
	}
	if err != nil {
		return err
	}
	if p.Manifest, err = Decode(data); err != nil {
		return err
	}
	return p.validate()
}

// Decode parses a manifest, refusing unknown fields: a typo must not be silently ignored.
func Decode(data []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("manifest.json: %w", err)
	}
	return m, nil
}

// Media returns a seekable reader over a pack file, suitable for http.ServeContent.
// Zip media are read in place, without extraction.
func (p *Pack) Media(name string) (io.ReadSeekCloser, error) {
	if !validPath(name) || !IsMedia(name) {
		return nil, fs.ErrInvalid
	}
	if p.zipFiles == nil {
		f, err := p.fsys.Open(name)
		if err != nil {
			return nil, err
		}
		return f.(io.ReadSeekCloser), nil
	}
	zf, ok := p.zipFiles[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if zf.Method != zip.Store {
		return nil, fmt.Errorf("%s is compressed in the archive", name)
	}
	off, _ := zf.DataOffset() // checked when the pack was loaded
	return nopCloser{io.NewSectionReader(p.file, off, int64(zf.CompressedSize64))}, nil
}

type nopCloser struct{ io.ReadSeeker }

func (nopCloser) Close() error { return nil }

// validPath accepts slash-separated relative paths only: no "..", no absolute or Windows-style paths.
func validPath(name string) bool {
	return fs.ValidPath(name) && name != "." && !strings.ContainsAny(name, `\:`)
}
