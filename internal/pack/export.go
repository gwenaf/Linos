package pack

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// FFmpeg runs ffmpeg and returns its output. Tests replace it; exec reports a missing ffmpeg.
var FFmpeg = func(args ...string) ([]byte, error) {
	return exec.Command("ffmpeg", args...).CombinedOutput()
}

var (
	audioExt = regexp.MustCompile(`(?i)\.(mp3|m4a|aac|ogg|opus|flac|wav)$`)
	videoExt = regexp.MustCompile(`(?i)\.(mp4|m4v|webm|mov|mkv|avi)$`)
)

const defaultDuration = 30

// Export writes the folder pack dir as a .linospack archive at dest. When ffmpeg is available, each
// audio or video track is cut to what the game can play: from its start, for the longest duration
// its rounds allow plus its outro. Audio is copied without re-encoding; video is re-encoded to
// H.264/AAC so the cut is exact and every browser plays it. Without ffmpeg, media are copied whole.
// It reports whether media were cut.
func Export(dir, dest string) (bool, error) {
	p, err := Open(dir)
	if err != nil {
		return false, err
	}
	p.Close() // a folder pack holds no open file
	m := p.Manifest
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".export-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmp)

	_, probeErr := FFmpeg("-version")
	cut := probeErr == nil
	files := map[string]string{} // archive name: source file
	longest := longestDuration(m)
	for i := range m.Tracks {
		t := &m.Tracks[i]
		video := videoExt.MatchString(t.Media)
		if !cut || !video && !audioExt.MatchString(t.Media) {
			files[t.Media] = filepath.Join(dir, t.Media)
			continue
		}
		ext := filepath.Ext(t.Media)
		args := []string{"-v", "error", "-y", "-ss", seconds(t.Start), "-i", filepath.Join(dir, t.Media), "-t", seconds(cutLength(*t, longest))}
		if video {
			ext = ".mp4"
			args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-c:a", "aac", "-movflags", "+faststart")
		} else {
			args = append(args, "-vn", "-c:a", "copy")
		}
		name := fmt.Sprintf("cut/%03d%s", i+1, ext)
		out := filepath.Join(tmp, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(out), 0o755) // ffmpeg reports a missing folder
		if msg, err := FFmpeg(append(args, out)...); err != nil {
			return true, fmt.Errorf("ffmpeg on track %s: %v: %s", t.ID, err, strings.TrimSpace(string(msg)))
		}
		files[name], t.Media, t.Start = out, name, 0
	}
	for _, name := range referencedImages(m) {
		files[name] = filepath.Join(dir, name)
	}

	archive := filepath.Join(tmp, "pack.linospack")
	if err := writeArchive(archive, m, files); err != nil {
		return cut, err
	}
	return cut, os.Rename(archive, dest)
}

// cutLength is the media time the game may play: its duration, then its outro, at its playback rate.
func cutLength(t Track, longest float64) float64 {
	d, rate := t.Duration, t.PlaybackRate
	if d == 0 {
		d = longest
	}
	if rate == 0 {
		rate = 1
	}
	return (d + t.Outro) * rate
}

// longestDuration bounds the duration of a track without its own: the pack's, or a round's if longer.
func longestDuration(m Manifest) float64 {
	d := float64(defaultDuration)
	if m.Rules != nil && m.Rules.Duration != nil {
		d = *m.Rules.Duration
	}
	for _, r := range m.Rounds {
		if r.Rules != nil && r.Rules.Duration != nil {
			d = max(d, *r.Rules.Duration)
		}
	}
	return d
}

// referencedImages lists the cover and the reveal pictures.
func referencedImages(m Manifest) []string {
	var names []string
	if m.Cover != "" {
		names = append(names, m.Cover)
	}
	for _, t := range m.Tracks {
		for _, r := range t.Reveal {
			if r.Image != nil && *r.Image != "" {
				names = append(names, *r.Image)
			}
		}
	}
	return names
}

// writeArchive stores the manifest and files uncompressed: the game reads media in place.
func writeArchive(path string, m Manifest, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	manifest, _ := json.MarshalIndent(m, "", "  ") // plain structs always marshal
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Store})
	if err == nil {
		_, err = w.Write(manifest)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err != nil {
			break
		}
		err = addFile(zw, name, files[name])
	}
	return errors.Join(err, zw.Close(), f.Close())
}

func addFile(zw *zip.Writer, name, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err == nil {
		_, err = io.Copy(w, in)
	}
	return err
}

func seconds(s float64) string {
	return strconv.FormatFloat(s, 'f', 3, 64)
}
