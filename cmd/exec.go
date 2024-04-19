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
	"errors"
	"fmt"
	"os"
	"path"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/mdbooth/polymorph/pkg/binary"
	"github.com/mdbooth/polymorph/pkg/tarball"
	"github.com/mdbooth/polymorph/pkg/templates"
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

	TarballFetcher *tarball.Fetcher `toml:"tarball"`
	BinaryFetcher  *binary.Fetcher  `toml:"binary"`
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

	directory, err := templates.ExpandTemplate(execTemplate.Directory, execTemplate.Params)
	if err != nil {
		return "", nil, fmt.Errorf("error expanding directory template: %s", err)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("error getting user cache dir: %s", err)
	}
	execCacheDir := path.Join(cacheDir, "polymorph", execTemplate.Name)
	versionedCacheDir := path.Join(execCacheDir, directory)

	executableBase := path.Base(executableName)
	fetcher := func() error {
		var err error
		err = os.MkdirAll(execCacheDir, 0755)
		if err != nil {
			return fmt.Errorf("error creating cache dir %s: %w", cacheDir, err)
		}

		tempDir, err := os.MkdirTemp(execCacheDir, directory)
		if err != nil {
			return fmt.Errorf("error creating temporary directory %s: %w", path.Join(cacheDir, directory), err)
		}
		defer os.RemoveAll(tempDir)

		switch {
		case execTemplate.TarballFetcher != nil:
			err = tarball.Fetch(execTemplate.TarballFetcher, execTemplate.Params, tempDir)

		case execTemplate.BinaryFetcher != nil:
			err = binary.Fetch(execTemplate.BinaryFetcher, execTemplate.Params, tempDir, executableBase)
		default:
			return fmt.Errorf("no fetcher specified")
		}

		if err != nil {
			return err
		}

		return os.Rename(tempDir, versionedCacheDir)
	}

	executable, ok := execTemplate.Executables[executableBase]
	if !ok {
		executable = executableBase
	}

	return path.Join(versionedCacheDir, executable), fetcher, nil
}

func init() {
	rootCmd.AddCommand(execCmd)
}
