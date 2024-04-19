package tarball

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"

	"github.com/mdbooth/polymorph/pkg/templates"
)

type Fetcher struct {
	URL string `toml:"url"`
}

func Fetch(fetcher *Fetcher, params map[string]string, tempDir string) error {
	var err error
	tarballURL, err := templates.ExpandTemplate(fetcher.URL, params)
	if err != nil {
		return fmt.Errorf("error expanding tarball fetch url template: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Downloading tarball from %s...\n", tarballURL)
	resp, err := http.Get(tarballURL)
	if err != nil {
		return fmt.Errorf("error downloading tarball %s: %w", tarballURL, err)
	}
	defer resp.Body.Close()

	uncompressed, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("error creating gzip reader: %w", err)
	}
	defer uncompressed.Close()

	return untar(uncompressed, tempDir)
}

func untar(r io.Reader, dir string) error {
	tarReader := tar.NewReader(r)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("error reading tarball: %w", err)
		}

		target := path.Join(dir, header.Name)
		mode := os.FileMode(header.Mode)

		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, mode)
			if err != nil {
				return fmt.Errorf("error creating directory %s: %w", target, err)
			}

		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("error creating file %s: %w", target, err)
			}

			if _, err := io.Copy(f, tarReader); err != nil {
				return fmt.Errorf("error writing file %s: %w", target, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("error closing file %s: %w", target, err)
			}
		}
	}
}
