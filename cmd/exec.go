/*
Copyright © 2024 Matthew Booth

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/
package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"syscall"
	"text/template"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:   "exec",
	Short: "Execute a binary",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		err := runExec(cmd, args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			os.Exit(1)
		}
	},
	Args: cobra.MinimumNArgs(2),
}

type ExecTemplate struct {
	Name        string            `toml:"name"`
	Directory   string            `toml:"directory"`
	Params      map[string]string `toml:"params"`
	Executables map[string]string `toml:"executables"`

	TarballFetcher TarballFetcher `toml:"tarball"`
}

type TarballFetcher struct {
	URL string `toml:"url"`
}

func runExec(_ *cobra.Command, args []string) error {
	var err error

	templateFile := args[0]
	executableName := args[1]

	execPath, fetchFunc, err := getConfig(executableName, templateFile)
	if err != nil {
		return err
	}

	execArgs := args[1:]
	err = syscall.Exec(execPath, execArgs, os.Environ())
	if !errors.Is(err, syscall.ENOENT) {
		return fmt.Errorf("error executing %s: %w", executableName, err)
	}

	if fetchFunc == nil {
		return fmt.Errorf("no fetcher specified")
	}
	err = fetchFunc()
	if err != nil {
		return fmt.Errorf("error fetching %s: %w", executableName, err)
	}

	// Should not return
	err = syscall.Exec(execPath, execArgs, os.Environ())
	return fmt.Errorf("error executing %s: %w", executableName, err)
}

func readTemplateFile(path string) (*ExecTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	template := ExecTemplate{}
	if _, err := toml.Decode(string(data), &template); err != nil {
		return nil, err
	}
	return &template, nil
}

func getConfig(executableName string, templateFile string) (string, func() error, error) {
	// Read templateFile into a Template struct
	execTemplate, err := readTemplateFile(templateFile)
	if err != nil {
		return "", nil, fmt.Errorf("error reading template %s: %s", templateFile, err)
	}

	directoryTmpl, err := template.New("directory").Parse(execTemplate.Directory)
	if err != nil {
		return "", nil, fmt.Errorf("error parsing directory template from %s: %s", templateFile, err)
	}

	// create a writer which writes to the string variable directory
	var directoryBytes bytes.Buffer
	if err := directoryTmpl.Execute(&directoryBytes, execTemplate.Params); err != nil {
		return "", nil, fmt.Errorf("error executing directory template from %s: %s", templateFile, err)
	}
	directory := directoryBytes.String()

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("error getting user cache dir: %s", err)
	}
	execCacheDir := path.Join(cacheDir, "polymorph", execTemplate.Name)
	versionedCacheDir := path.Join(execCacheDir, directory)

	fetcher := func() error {
		return tarballFetcher(&execTemplate.TarballFetcher, execCacheDir, directory, execTemplate.Params)
	}

	executableBase := path.Base(executableName)
	executable, ok := execTemplate.Executables[executableBase]
	if !ok {
		executable = executableBase
	}

	return path.Join(versionedCacheDir, executable), fetcher, nil
}

func tarballFetcher(fetcher *TarballFetcher, cacheDir, directory string, params map[string]string) error {
	var err error
	err = os.MkdirAll(cacheDir, 0755)
	if err != nil {
		return fmt.Errorf("error creating cache dir %s: %w", cacheDir, err)
	}

	tempDir, err := os.MkdirTemp(cacheDir, directory)
	if err != nil {
		return fmt.Errorf("error creating temporary directory %s: %w", path.Join(cacheDir, directory), err)
	}
	defer os.RemoveAll(tempDir)

	tarballURL, err := expandTemplate(fetcher.URL, params)
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

	if err := untar(uncompressed, tempDir); err != nil {
		return err
	}

	targetDir := path.Join(cacheDir, directory)
	return os.Rename(tempDir, targetDir)
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

func expandTemplate(templateString string, params map[string]string) (string, error) {
	t, err := template.New("").Parse(templateString)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = t.Execute(&buf, params)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func init() {
	rootCmd.AddCommand(execCmd)
}
