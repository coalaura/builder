package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func cgoIncludeFlags(req *Request) (string, error) {
	if !req.CGO {
		return "", nil
	}

	directories, err := resolveIncludeDirs(req)
	if err != nil {
		return "", err
	}

	size := 0

	for _, directory := range directories {
		size += len(directory) + 5
	}

	var flags strings.Builder

	flags.Grow(size)

	for _, directory := range directories {
		// Go's environment flag parser accepts quoted words but does not unescape them.
		quote := byte('\'')

		if strings.ContainsRune(directory, '\'') {
			quote = '"'

			if strings.ContainsRune(directory, '"') {
				return "", fmt.Errorf("include directory contains both single and double quotes: %s", directory)
			}
		}

		if flags.Len() != 0 {
			flags.WriteByte(' ')
		}

		flags.WriteByte(quote)
		flags.WriteString("-I")
		flags.WriteString(directory)
		flags.WriteByte(quote)
	}

	return flags.String(), nil
}

func resolveIncludeDirs(req *Request) ([]string, error) {
	directories := make([]string, 0, len(req.IncludeDirs)+2)

	for _, directory := range req.IncludeDirs {
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(req.Cwd, directory)
		}

		directories = append(directories, filepath.Clean(directory))
	}

	// Explicit paths take precedence over the project's conventional directories.
	directories = append(directories, filepath.Join(req.Project, "include"), filepath.Join(req.Project, "includes"))

	seen := make(map[string]bool, len(directories))

	var count int

	for index, directory := range directories {
		info, err := os.Stat(directory)
		if index >= len(req.IncludeDirs) && (os.IsNotExist(err) || (err == nil && !info.IsDir())) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("include directory %q: %w", directory, err)
		}

		if !info.IsDir() {
			return nil, fmt.Errorf("include directory %q is not a directory", directory)
		}

		key := directory

		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}

		if seen[key] {
			continue
		}

		seen[key] = true

		directories[count] = directory
		count++
	}

	return directories[:count], nil
}
