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
// Entries with an unsafe path are refused: an archive cannot write outside dir.
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
		if f.FileInfo().IsDir() {
			continue
		}
		if !validPath(f.Name) {
			err = fmt.Errorf("unsafe path %q in the archive", f.Name)
		} else {
			err = extract(f, filepath.Join(dir, filepath.FromSlash(f.Name)))
		}
		if err != nil {
			os.RemoveAll(dir)
			return err
		}
	}
	return nil
}

func extract(f *zip.File, dest string) error {
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
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}
