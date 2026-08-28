package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/coalaura/builder/goenv"
)

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

func resolveTagArgs(buildFlags []string) []string {
	for i, arg := range buildFlags {
		if arg == "-tags" && i+1 < len(buildFlags) {
			return buildFlags[i : i+2]
		}
	}

	return nil
}

func resolveRunBuildArgs(req *Request, cfg goenv.Config) []string {
	args := resolveTagArgs(cfg.BuildFlags)

	if req.GUI && req.TargetOS == "windows" {
		args = append(args, "-ldflags", "-H windowsgui")
	}

	return args
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
