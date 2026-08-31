package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Request struct {
	Command       string
	Language      string
	TargetOS      string
	Target        string
	Package       string
	Output        string
	SigningKey    string
	SigningChains []string
	Passphrase    string
	CGO           bool
	Dynamic       bool
	Compatible    bool
	Minify        bool
	GUI           bool
	NoGenerate    bool
	Debug         bool
	GoFlags       []string
	Forward       []string
	Cwd           string
	Project       string
	RunTarget     string
}

func parseRequest(command string, args, languages []string, allowOS bool) (*Request, error) {
	req := &Request{Command: command}

	allowed := make(map[string]bool, len(languages))

	for _, language := range languages {
		allowed[language] = true
	}

	var (
		forwarding          bool
		cgoRequested        bool
		pureRequested       bool
		dynamicRequested    bool
		staticRequested     bool
		compatibleRequested bool
		optimizeRequested   bool
		minifyRequested     bool
		noMinifyRequested   bool
		generateRequested   bool
		noGenerateRequested bool
	)

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

		if lower == "-o" && command == "build" {
			if i+1 >= len(args) || args[i+1] == "--" {
				return nil, fmt.Errorf("%s requires a value", lower)
			}

			i++

			req.Output = args[i]

			continue
		}

		if strings.HasPrefix(lower, "-o=") && command == "build" {
			_, req.Output, _ = strings.Cut(arg, "=")
			if req.Output == "" {
				return nil, fmt.Errorf("-o requires a value")
			}

			continue
		}

		goFlag, takesValue, recognized := parseGoFlag(command, arg)
		if recognized {
			req.GoFlags = append(req.GoFlags, goFlag)

			if takesValue {
				if i+1 >= len(args) || args[i+1] == "--" {
					return nil, fmt.Errorf("%s requires a value", goFlag)
				}

				i++

				req.GoFlags = append(req.GoFlags, args[i])
			}

			continue
		}

		if lower == "--package" || lower == "--pkg" {
			if i+1 >= len(args) || args[i+1] == "--" {
				return nil, fmt.Errorf("%s requires a value", lower)
			}

			i++
			req.Package = args[i]

			continue
		}

		if lower == "--output" || lower == "--out" {
			if command != "build" {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			if i+1 >= len(args) || args[i+1] == "--" {
				return nil, fmt.Errorf("%s requires a value", lower)
			}

			i++
			req.Output = args[i]

			continue
		}

		if lower == "--sign" {
			if command != "build" {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			if i+1 >= len(args) || args[i+1] == "--" {
				return nil, fmt.Errorf("%s requires a value", lower)
			}

			i++
			req.SigningKey = args[i]

			continue
		}

		if lower == "--sign-chain" {
			if command != "build" {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			if i+1 >= len(args) || args[i+1] == "--" {
				return nil, fmt.Errorf("%s requires a value", lower)
			}

			i++
			req.SigningChains = append(req.SigningChains, args[i])

			continue
		}

		if lower == "--passphrase" {
			if command != "build" {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			if i+1 >= len(args) || args[i+1] == "--" {
				return nil, fmt.Errorf("%s requires a value", lower)
			}

			i++

			req.Passphrase = args[i]

			continue
		}

		if strings.HasPrefix(lower, "--package=") || strings.HasPrefix(lower, "--pkg=") {
			_, req.Package, _ = strings.Cut(arg, "=")
			if req.Package == "" {
				return nil, fmt.Errorf("%s requires a value", strings.SplitN(lower, "=", 2)[0])
			}

			continue
		}

		if strings.HasPrefix(lower, "--output=") || strings.HasPrefix(lower, "--out=") {
			if command != "build" {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			_, req.Output, _ = strings.Cut(arg, "=")
			if req.Output == "" {
				return nil, fmt.Errorf("%s requires a value", strings.SplitN(lower, "=", 2)[0])
			}

			continue
		}

		if strings.HasPrefix(lower, "--sign=") {
			if command != "build" {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			_, req.SigningKey, _ = strings.Cut(arg, "=")
			if req.SigningKey == "" {
				return nil, fmt.Errorf("--sign requires a value")
			}

			continue
		}

		if strings.HasPrefix(lower, "--sign-chain=") {
			if command != "build" {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			_, signingChain, _ := strings.Cut(arg, "=")
			if signingChain == "" {
				return nil, fmt.Errorf("--sign-chain requires a value")
			}

			req.SigningChains = append(req.SigningChains, signingChain)

			continue
		}

		if strings.HasPrefix(lower, "--passphrase=") {
			if command != "build" {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			_, req.Passphrase, _ = strings.Cut(arg, "=")
			if req.Passphrase == "" {
				return nil, fmt.Errorf("--passphrase requires a value")
			}

			continue
		}

		switch lower {
		case "--cgo":
			cgoRequested = true
			req.CGO = true
		case "--pure":
			pureRequested = true
			req.CGO = false
		case "--no-gen", "--no-generate":
			noGenerateRequested = true
			req.NoGenerate = true
		case "--gen", "--generate":
			generateRequested = true
			req.NoGenerate = false
		case "--debug":
			req.Debug = true
		case "--dyn", "--dynamic":
			dynamicRequested = true
			req.Dynamic = true
		case "--stat", "--static":
			staticRequested = true
			req.Dynamic = false
		case "--compat", "--compatible":
			compatibleRequested = true
			req.Compatible = true
		case "--opt", "--optimize":
			optimizeRequested = true
			req.Compatible = false
		case "--min", "--minify":
			minifyRequested = true
			req.Minify = true
		case "--no-min", "--no-minify":
			noMinifyRequested = true
			req.Minify = false
		case "--gui":
			if command != "build" && command != "run" {
				return nil, fmt.Errorf("unknown argument for %s: %s", command, arg)
			}

			req.GUI = true
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

	switch {
	case cgoRequested && pureRequested:
		return nil, fmt.Errorf("--cgo and --pure cannot be used together")
	case dynamicRequested && staticRequested:
		return nil, fmt.Errorf("--dynamic and --static cannot be used together")
	case compatibleRequested && optimizeRequested:
		return nil, fmt.Errorf("--compatible and --optimize cannot be used together")
	case minifyRequested && noMinifyRequested:
		return nil, fmt.Errorf("--minify and --no-minify cannot be used together")
	case generateRequested && noGenerateRequested:
		return nil, fmt.Errorf("--generate and --no-generate cannot be used together")
	case !req.CGO && req.Dynamic:
		return nil, fmt.Errorf("--dynamic requires --cgo")
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

	if req.Output != "" && req.Language != "go" {
		return nil, fmt.Errorf("--output is only supported for go builds")
	}

	if len(req.GoFlags) != 0 && req.Language != "go" {
		return nil, fmt.Errorf("go flags are only supported for Go projects")
	}

	if req.SigningKey != "" && req.Language != "go" {
		return nil, fmt.Errorf("--sign is only supported for Go builds")
	}

	if len(req.SigningChains) != 0 && req.SigningKey == "" {
		return nil, fmt.Errorf("--sign-chain requires --sign")
	}

	if req.Passphrase != "" && req.SigningKey == "" {
		return nil, fmt.Errorf("--passphrase requires --sign")
	}

	return req, nil
}

func parseGoFlag(command, arg string) (string, bool, bool) {
	if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return "", false, false
	}

	name, _, joined := strings.Cut(arg, "=")
	name = strings.TrimPrefix(name, "-")

	takesValue, recognized := goFlagType(name)
	if !recognized || !supportsGoFlag(command, name) {
		return "", false, false
	}

	if joined {
		return arg, false, true
	}

	return arg, takesValue, true
}

func goFlagType(name string) (bool, bool) {
	switch name {
	case "a", "asan", "benchmem", "buildvcs", "c", "cover", "failfast", "fullpath", "json", "linkshared",
		"modcacherw", "msan", "n", "race", "short", "trimpath", "v", "work", "x":
		return false, true
	case "asmflags", "bench", "benchtime", "blockprofile", "blockprofilerate", "buildmode", "compiler", "count",
		"covermode", "coverpkg", "coverprofile", "cpu", "cpuprofile", "exec", "fuzz", "fuzzminimizetime",
		"fuzztime", "gccgoflags", "gcflags", "installsuffix", "ldflags", "list", "memprofile",
		"memprofilerate", "mod", "modfile", "mutexprofile", "mutexprofilefraction", "o", "outputdir", "overlay",
		"p", "parallel", "pgo", "pkgdir", "run", "shuffle", "skip", "tags", "timeout", "toolexec", "trace", "vet":
		return true, true
	default:
		return false, false
	}
}

func supportsGoFlag(command, name string) bool {
	switch name {
	case "bench", "benchtime", "benchmem", "blockprofile", "blockprofilerate", "c", "count", "coverprofile",
		"cpu", "cpuprofile", "failfast", "fullpath", "fuzz", "fuzzminimizetime", "fuzztime", "json", "list",
		"memprofile", "memprofilerate", "mutexprofile", "mutexprofilefraction", "outputdir", "parallel", "run",
		"short", "shuffle", "skip", "timeout", "trace", "vet":
		return command == "test" || command == "bench"
	case "exec":
		return command == "run" || command == "test" || command == "bench"
	case "o":
		return command == "test" || command == "bench"
	default:
		return true
	}
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
