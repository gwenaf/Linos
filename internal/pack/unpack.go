package pack

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Unpack extracts a .linospack archive into the new folder dir, so it can be edited again.
// Entries with an unsafe path are refused: an archive cannot write outside dir. Only the manifest and
// media are extracted, both with a bounded size.
func Unpack(archive, dir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	for _, f := range zr.File {
		dest := filepath.Join(dir, filepath.FromSlash(f.Name))
		switch {
		case f.FileInfo().IsDir():
			continue
		case !validPath(f.Name):
			err = fmt.Errorf("unsafe path %q in the archive", f.Name)
		case f.Name == "manifest.json":
			err = extract(f, dest, MaxManifest)
		case !IsMedia(f.Name):
			continue // only the manifest and media: an archive cannot drop a program into packs/
		case f.Method != zip.Store:
			err = fmt.Errorf("%s is compressed in the archive", f.Name)
		default:
			err = extract(f, dest, int64(f.CompressedSize64))
		}
		if err != nil {
			os.RemoveAll(dir)
			return err
		}
	}
	return nil
}

// extract writes f to dest, refusing more than limit bytes: a compressed entry may lie about its size.
func extract(f *zip.File, dest string, limit int64) error {
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	os.MkdirAll(filepath.Dir(dest), 0o755) // Create reports any failure
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(in, limit+1))
	if err == nil && n > limit {
		err = fmt.Errorf("%s is larger than %d bytes", f.Name, limit)
	}
	return errors.Join(err, out.Close())
}
