package main

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func RunProcess(dir string, env map[string]string, name string, args ...string) error {
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

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return &ExitError{code: exit.ExitCode()}
	}

	return err
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
