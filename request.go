package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Request struct {
	Command    string
	Language   string
	TargetOS   string
	Target     string
	Package    string
	Pure       bool
	Dynamic    bool
	Compatible bool
	Minify     bool
	Forward    []string
	Cwd        string
	Project    string
	RunTarget  string
}

func parseRequest(command string, args, languages []string, allowOS bool) (*Request, error) {
	req := &Request{Command: command}

	allowed := make(map[string]bool, len(languages))

	for _, language := range languages {
		allowed[language] = true
	}

	var forwarding bool

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if forwarding {
			req.Forward = append(req.Forward, arg)

			continue
		}

		if arg == "--" {
			forwarding = true

			continue
		}

		lower := strings.ToLower(arg)

		if lower == "--package" || lower == "--pkg" {
			if i+1 >= len(args) || args[i+1] == "--" {
				return nil, fmt.Errorf("%s requires a value", lower)
			}

			i++
			req.Package = args[i]

			continue
		}

		if strings.HasPrefix(lower, "--package=") || strings.HasPrefix(lower, "--pkg=") {
			_, req.Package, _ = strings.Cut(arg, "=")
			if req.Package == "" {
				return nil, fmt.Errorf("%s requires a value", strings.SplitN(lower, "=", 2)[0])
			}

			continue
		}

		switch lower {
		case "--pure":
			req.Pure = true
		case "--dyn", "--dynamic":
			req.Dynamic = true
		case "--compat", "--compatible":
			req.Compatible = true
		case "--opt", "--optimize":
			// Optimized builds are the default; this is the explicit spelling.
		case "--min", "--minify":
			req.Minify = true
		case "win", "windows", "lin", "linux", "dar", "darwin":
			if !allowOS {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			req.TargetOS = normalizeOS(lower)
		default:
			if allowed[lower] {
				req.Language = lower
			} else if req.Target == "" && looksLikeFilepath(arg) {
				req.Target = arg
			} else {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}
		}
	}

	if req.Pure && req.Dynamic {
		return nil, fmt.Errorf("--pure and --dynamic cannot be used together")
	}

	var err error

	req.Cwd, err = filepath.Abs(".")
	if err != nil {
		return nil, err
	}

	if req.TargetOS == "" {
		req.TargetOS = runtime.GOOS
	}

	if req.Language == "" {
		detectionDir := req.Cwd

		if req.Target != "" {
			detectionDir, _ = resolveProjectTarget(command, req.Cwd, req.Target, "")
		}

		req.Language = detectLanguage(command, detectionDir)
	}

	req.Project, req.RunTarget = resolveProjectTarget(command, req.Cwd, req.Target, req.Language)
	if req.Package != "" {
		req.RunTarget = req.Package
	}

	return req, nil
}

func normalizeOS(value string) string {
	switch value {
	case "win", "windows":
		return "windows"
	case "lin", "linux":
		return "linux"
	case "dar", "darwin":
		return "darwin"
	}

	return runtime.GOOS
}

func looksLikeFilepath(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") {
		return false
	}

	if strings.HasPrefix(value, "./") || strings.HasPrefix(value, `.\`) || strings.ContainsAny(value, `/\`) {
		return true
	}

	if len(filepath.Ext(value)) > 1 {
		return true
	}

	_, err := os.Stat(value)
	return err == nil
}

func getScriptName(command string) string {
	if runtime.GOOS == "windows" {
		return command + ".cmd"
	}

	return command + ".sh"
}

func detectLanguage(command, dir string) string {
	if doesFileExists(filepath.Join(dir, getScriptName(command))) {
		return "script"
	}

	if command == "run" && doesFileExists(filepath.Join(dir, "artisan")) {
		return "php"
	}

	if doesFileExists(filepath.Join(dir, "go.mod")) {
		return "go"
	}

	if doesFileExists(filepath.Join(dir, "package.json")) {
		return "js"
	}

	if command == "run" {
		if findFirstExistingFile(dir, []string{"index.js", "main.js", "app.js"}) != "" {
			return "js"
		}
	}

	return ""
}

func resolveProjectTarget(command, cwd, target, language string) (string, string) {
	if target == "" {
		return cwd, ""
	}

	resolved, err := filepath.Abs(target)
	if err != nil {
		return cwd, target
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return cwd, target
	}

	cwdProject := isValidProject(command, cwd, language)

	if !info.IsDir() {
		if cwdProject {
			return cwd, resolved
		}

		return filepath.Dir(resolved), resolved
	}

	if isValidProject(command, resolved, language) {
		return resolved, ""
	}

	if cwdProject {
		return cwd, target
	}

	return resolved, ""
}

func isValidProject(command, dir, language string) bool {
	switch language {
	case "go":
		return doesFileExists(filepath.Join(dir, "go.mod"))
	case "js":
		return doesFileExists(filepath.Join(dir, "package.json"))
	case "php":
		return doesFileExists(filepath.Join(dir, "artisan"))
	case "script":
		return doesFileExists(filepath.Join(dir, getScriptName(command)))
	}

	return false
}

func doesFileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
}

func findFirstExistingFile(dir string, names []string) string {
	for _, name := range names {
		if doesFileExists(filepath.Join(dir, name)) {
			return name
		}
	}

	return ""
}
