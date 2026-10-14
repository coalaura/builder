package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

const darwinCompilerCommand = "_darwin-compiler"

func newDarwinCompilerCommand() *cli.Command {
	return &cli.Command{
		Name:            darwinCompilerCommand,
		Hidden:          true,
		SkipFlagParsing: true,
		Action: func(_ context.Context, command *cli.Command) error {
			directory, err := os.Getwd()
			if err != nil {
				return err
			}

			arguments := command.Args().Slice()

			err = normalizeDarwinSearchPaths(arguments, directory)
			if err != nil {
				return err
			}

			return RunProcess(false, directory, nil, "zig", arguments...)
		},
	}
}

func wrapDarwinCompilers(environment map[string]string) error {
	if environment["GOOS"] != "darwin" || !strings.Contains(environment["CC"], " --sysroot ") {
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	command, err := quoteCompilerExecutable(executable)
	if err != nil {
		return err
	}

	command += " " + darwinCompilerCommand

	compilers := [...]string{"CC", "CXX"}

	for _, compiler := range compilers {
		environment[compiler] = command + strings.TrimPrefix(environment[compiler], "zig")
	}

	return nil
}

func normalizeDarwinSearchPaths(arguments []string, directory string) error {
	var sdk string

	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]

		if argument == "--sysroot" && index+1 < len(arguments) {
			index++
			sdk = arguments[index]

			continue
		}

		value, found := strings.CutPrefix(argument, "--sysroot=")
		if found {
			sdk = value
		}
	}

	if sdk == "" {
		return nil
	}

	if !filepath.IsAbs(sdk) {
		sdk = filepath.Join(directory, sdk)
	}

	// Relative compiler paths are evaluated from the physical working directory.
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}

	directory = resolved

	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if !strings.HasPrefix(argument, "-L") && !strings.HasPrefix(argument, "-F") {
			continue
		}

		prefix := argument[:2]
		path := argument[2:]

		if path == "" && index+1 < len(arguments) {
			index++
			path = arguments[index]
			prefix = ""
		}

		relative, err := darwinSearchPath(path, directory, sdk)
		if err != nil {
			return err
		}

		arguments[index] = prefix + relative
	}

	return nil
}

func darwinSearchPath(path, directory, sdk string) (string, error) {
	if !filepath.IsAbs(path) {
		return path, nil
	}

	// Keep SDK paths virtual so Zig can resolve system libraries and their re-exports.
	info, err := os.Stat(filepath.Join(sdk, path))
	if err == nil && info.IsDir() {
		return path, nil
	}

	info, err = os.Stat(path)
	if err != nil || !info.IsDir() {
		return path, nil
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	// Only absolute search paths are rebased by Zig; relative paths reach local libraries.
	relative, err := filepath.Rel(directory, resolved)
	if err != nil {
		return "", fmt.Errorf("make darwin search path relative: %w", err)
	}

	return relative, nil
}

func quoteCompilerExecutable(path string) (string, error) {
	if !strings.ContainsAny(path, " \t\n\r'\"") {
		return path, nil
	}

	if !strings.ContainsRune(path, '\'') {
		return "'" + path + "'", nil
	}

	if !strings.ContainsRune(path, '"') {
		return "\"" + path + "\"", nil
	}

	return "", fmt.Errorf("compiler executable path contains both quote characters: %s", path)
}
