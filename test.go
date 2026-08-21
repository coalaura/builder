package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type TestEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

func ExecuteTest(req *Request) error {
	switch req.Language {
	case "go":
		err := GenerateGo(req.Project)
		if err != nil {
			return err
		}

		cfg := PrepareGo(req)

		target := req.RunTarget
		if target == "" {
			target = "./..."
		}

		Infof("[go] testing %s (mode: %s)", target, cfg.Mode)

		args := []string{"test", "-json"}

		args = append(args, resolveTagArgs(cfg.BuildFlags)...)
		args = append(args, cfg.Extra...)
		args = append(args, target)

		return runGoTest(req.Project, cfg.Env, args)
	case "js":
		if req.RunTarget != "" && doesFileExists(req.RunTarget) {
			Infof("[bun test] testing %s", req.RunTarget)

			args := []string{"test", req.RunTarget}

			args = append(args, req.Forward...)

			return RunProcess(req.Project, nil, "bun", args...)
		}

		script := findPackageJsonScript(req.Project, []string{"test"})
		if script != "" {
			Infof("[bun/%s] testing %s", script, req.Project)

			args := []string{"run", script}

			args = append(args, req.Forward...)

			return RunProcess(req.Project, nil, "bun", args...)
		}

		matches, _ := filepath.Glob(filepath.Join(req.Project, "*.test.*"))
		specs, _ := filepath.Glob(filepath.Join(req.Project, "*.spec.*"))

		if len(matches)+len(specs) > 0 {
			Infof("[bun test] testing %s", req.Project)

			args := []string{"test"}

			args = append(args, req.Forward...)

			return RunProcess(req.Project, nil, "bun", args...)
		}

		return fmt.Errorf("%s is not a recognized js test project", req.Project)
	}

	return fmt.Errorf("%s is not a recognized test project", req.Project)
}

func runGoTest(dir string, env map[string]string, args []string) error {
	cmd := exec.Command("go", args...)

	cmd.Dir = dir
	cmd.Env = mergeEnvironment(env)
	cmd.Stdin = os.Stdin

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		return err
	}

	ran, passed, failed, scanErr := formatTestEvents(stdout)

	err = cmd.Wait()

	fmt.Printf("tests: %d ran, %d passed, %d failed\n", ran, passed, failed)

	if scanErr != nil {
		return scanErr
	}

	if err == nil {
		return nil
	}

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return &ExitError{code: exit.ExitCode()}
	}

	return err
}

func formatTestEvents(reader io.Reader) (int, int, int, error) {
	var (
		ran    int
		passed int
		failed int
	)

	scanner := bufio.NewScanner(reader)

	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		var event TestEvent

		err := json.Unmarshal(line, &event)
		if err != nil {
			fmt.Println(string(line))

			continue
		}

		elapsed := fmt.Sprintf("(%.2fs)", event.Elapsed)

		switch {
		case event.Test != "" && event.Action == "run":
			ran++

			Subf("run:  %s", event.Test)
		case event.Test != "" && event.Action == "pass":
			passed++

			Subf("pass: %s %s", event.Test, elapsed)
		case event.Test != "" && event.Action == "fail":
			failed++

			Subf("fail: %s %s", event.Test, elapsed)
		case event.Test != "" && event.Action == "skip":
			Subf("skip: %s %s", event.Test, elapsed)
		case event.Test == "" && event.Action == "pass":
			fmt.Printf("ok     %s %s\n", event.Package, elapsed)
		case event.Test == "" && event.Action == "fail":
			fmt.Printf("FAIL   %s %s\n", event.Package, elapsed)
		case event.Test == "" && event.Action == "skip":
			fmt.Printf("?      %s [no test files]\n", event.Package)
		case event.Action == "output" && !isGeneratedTestLine(event.Output):
			fmt.Print(event.Output)
		}
	}

	return ran, passed, failed, scanner.Err()
}

func isGeneratedTestLine(output string) bool {
	trimmed := strings.TrimSpace(output)

	return strings.HasPrefix(trimmed, "=== RUN") || strings.HasPrefix(trimmed, "--- PASS:") ||
		strings.HasPrefix(trimmed, "--- FAIL:") || strings.HasPrefix(trimmed, "--- SKIP:") ||
		trimmed == "PASS" || trimmed == "FAIL" || strings.HasPrefix(trimmed, "ok\t") ||
		strings.HasPrefix(trimmed, "ok  \t") || strings.HasPrefix(trimmed, "?\t") ||
		strings.HasPrefix(trimmed, "?   \t") || strings.HasPrefix(trimmed, "FAIL\t")
}
