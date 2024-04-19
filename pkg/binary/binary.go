package binary

import (
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

func Fetch(fetcher *Fetcher, params map[string]string, tempDir, executable string) error {
	var err error

	binaryURL, err := templates.ExpandTemplate(fetcher.URL, params)
	if err != nil {
		return fmt.Errorf("error expanding binary fetch url template: %w", err)
	}

	filePath := path.Join(tempDir, executable)
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return fmt.Errorf("error opening file %s: %w", filePath, err)
	}
	defer f.Close()

	fmt.Fprintf(os.Stderr, "Downloading binary from %s...\n", binaryURL)
	resp, err := http.Get(binaryURL)
	if err != nil {
		return fmt.Errorf("error downloading binary %s: %w", binaryURL, err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("error writing to %s: %w", filePath, err)
	}

	return f.Close()
}
