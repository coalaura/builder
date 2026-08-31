package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/coalaura/builder/goenv"
)

type GoFlag struct {
	Name   string
	Value  string
	Joined bool
}

func ExecuteRequest(req *Request) error {
	if req.Language == "script" {
		return ExecuteScript(req)
	}

	switch req.Command {
	case "bench":
		return ExecuteBench(req)
	case "test":
		return ExecuteTest(req)
	case "run":
		return ExecuteRun(req)
	case "build":
		return ExecuteBuild(req)
	}

	return fmt.Errorf("unknown command: %s", req.Command)
}

func ExecuteScript(req *Request) error {
	path := filepath.Join(req.Project, getScriptName(req.Command))

	Infof("[%s] %s %s", filepath.Base(path), getActionWord(req.Command), req.Project)

	if runtime.GOOS == "windows" {
		args := append([]string{"/d", "/c", "call", path}, req.Forward...)

		return RunProcess(req.Debug, req.Project, nil, "cmd.exe", args...)
	}

	if !req.Debug {
		info, err := os.Stat(path)
		if err == nil {
			os.Chmod(path, info.Mode()|0o100)
		}
	}

	return RunProcess(req.Debug, req.Project, nil, path, req.Forward...)
}

func GenerateGo(req *Request) error {
	if req.NoGenerate {
		return nil
	}

	Infof("[go] generating %s", req.Project)

	start := time.Now()

	err := RunProcess(req.Debug, req.Project, nil, "go", "generate", "./...")
	if err != nil {
		return err
	}

	if !req.Debug {
		printDuration(start, "generated")
	}

	return nil
}

func resolveGoFlags(req *Request, cfg goenv.Config, commandFlags ...string) []string {
	defaults := make([]string, 0, len(commandFlags)+len(cfg.BuildFlags)+2)

	defaults = append(defaults, commandFlags...)
	defaults = append(defaults, cfg.BuildFlags...)
	defaults = append(defaults, "-ldflags", cfg.LDFlags)

	return mergeGoFlags(defaults, req.GoFlags)
}

func mergeGoFlags(defaults, user []string) []string {
	flags := parseGoFlags(defaults)

	defaultIndexes := make(map[string]int, len(flags))

	for index, flag := range flags {
		defaultIndexes[flag.Name] = index
	}

	for _, flag := range parseGoFlags(user) {
		index, found := defaultIndexes[flag.Name]
		if !found {
			flags = append(flags, flag)

			continue
		}

		switch flag.Name {
		case "ldflags":
			if isQualifiedToolFlag(flag.Value) {
				flags = append(flags, flag)
			} else {
				flags[index].Value = strings.TrimSpace(flags[index].Value + " " + flag.Value)
			}
		case "tags":
			flags[index].Value = mergeCSV(flags[index].Value, flag.Value)
		case "json":
			// Test output parsing requires Go's JSON event stream.
		default:
			flags[index] = flag
			delete(defaultIndexes, flag.Name)
		}
	}

	args := make([]string, 0, len(flags)*2)

	for _, flag := range flags {
		if flag.Joined {
			args = append(args, "-"+flag.Name+"="+flag.Value)

			continue
		}

		args = append(args, "-"+flag.Name)

		if flag.Value != "" {
			args = append(args, flag.Value)
		}
	}

	return args
}

func isQualifiedToolFlag(value string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(value), " ")
	pattern, _, qualified := strings.Cut(first, "=")

	return qualified && !strings.HasPrefix(pattern, "-")
}

func parseGoFlags(args []string) []GoFlag {
	flags := make([]GoFlag, 0, len(args))

	for index := 0; index < len(args); index++ {
		arg := args[index]

		name, value, joined := strings.Cut(strings.TrimPrefix(arg, "-"), "=")

		flag := GoFlag{
			Name:   name,
			Value:  value,
			Joined: joined,
		}

		takesValue, _ := goFlagType(name)
		if takesValue && !joined && index+1 < len(args) {
			index++
			flag.Value = args[index]
		}

		flags = append(flags, flag)
	}

	return flags
}

func mergeCSV(values ...string) string {
	items := make([]string, 0)
	seen := make(map[string]bool)

	for _, value := range values {
		for item := range strings.SplitSeq(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" || seen[item] {
				continue
			}

			seen[item] = true
			items = append(items, item)
		}
	}

	return strings.Join(items, ",")
}

func printDuration(start time.Time, action string) {
	elapsed := time.Since(start)

	if elapsed < time.Second {
		Subf("%s in %dms", action, elapsed.Milliseconds())
	} else {
		Subf("%s in %.2fs", action, elapsed.Seconds())
	}
}

func getActionWord(command string) string {
	switch command {
	case "bench":
		return "benchmarking"
	case "test":
		return "testing"
	case "build":
		return "building"
	default:
		return "running"
	}
}
