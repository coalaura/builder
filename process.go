package main

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func RunProcess(debug bool, dir string, env map[string]string, name string, args ...string) error {
	if debug {
		Infof("[debug] %s", formatCommand(name, args))

		return nil
	}

	cmd := exec.Command(name, args...)

	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = mergeEnvironment(env)

	err := cmd.Run()
	if err == nil {
		return nil
	}

	exit, ok := errors.AsType[*exec.ExitError](err)
	if ok {
		return &ExitError{code: exit.ExitCode()}
	}

	return err
}

func formatCommand(name string, args []string) string {
	parts := make([]string, 0, len(args)+1)

	parts = append(parts, quoteCommandArg(name))

	for _, arg := range args {
		parts = append(parts, quoteCommandArg(arg))
	}

	return strings.Join(parts, " ")
}

func quoteCommandArg(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\r\n\"") {
		return value
	}

	return strconv.Quote(value)
}

func mergeEnvironment(overrides map[string]string) []string {
	values := make(map[string]string)
	keys := make(map[string]string)

	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}

		canonical := key

		if runtime.GOOS == "windows" {
			canonical = strings.ToUpper(key)
		}

		keys[canonical] = key
		values[canonical] = value
	}

	for key, value := range overrides {
		canonical := key

		if runtime.GOOS == "windows" {
			canonical = strings.ToUpper(key)
		}

		if value == "" {
			delete(keys, canonical)
			delete(values, canonical)

			continue
		}

		keys[canonical] = key
		values[canonical] = value
	}

	env := make([]string, 0, len(values))

	for canonical, value := range values {
		env = append(env, keys[canonical]+"="+value)
	}

	return env
}
