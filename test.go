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
		err := GenerateGo(req)
		if err != nil {
			return err
		}

		cfg := prepareGo(req)

		target := req.RunTarget
		if target == "" {
			target = "./..."
		}

		Infof("[go] testing %s (mode: %s)", target, cfg.Mode)

		args := []string{"test", "-json"}

		args = append(args, resolveTagArgs(cfg.BuildFlags)...)
		args = append(args, target)
		args = append(args, req.Forward...)

		return runGoTest(req.Debug, req.Project, cfg.Env, args)
	case "js":
		if req.RunTarget != "" && doesFileExists(req.RunTarget) {
			Infof("[bun test] testing %s", req.RunTarget)

			args := []string{"test", req.RunTarget}

			args = append(args, req.Forward...)

			return RunProcess(req.Debug, req.Project, nil, "bun", args...)
		}

		script := findPackageJsonScript(req.Project, []string{"test"})
		if script != "" {
			Infof("[bun/%s] testing %s", script, req.Project)

			args := []string{"run", script}

			args = append(args, req.Forward...)

			return RunProcess(req.Debug, req.Project, nil, "bun", args...)
		}

		matches, _ := filepath.Glob(filepath.Join(req.Project, "*.test.*"))
		specs, _ := filepath.Glob(filepath.Join(req.Project, "*.spec.*"))

		if len(matches)+len(specs) > 0 {
			Infof("[bun test] testing %s", req.Project)

			args := []string{"test"}

			args = append(args, req.Forward...)

			return RunProcess(req.Debug, req.Project, nil, "bun", args...)
		}

		return fmt.Errorf("%s is not a recognized js test project", req.Project)
	}

	return fmt.Errorf("%s is not a recognized test project", req.Project)
}

func runGoTest(debug bool, dir string, env map[string]string, args []string) error {
	if debug {
		Infof("[debug] %s", formatCommand("go", args))

		return nil
	}

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

	ran, passed, failed, skipped, scanErr := formatTestEvents(stdout, os.Stdout)

	err = cmd.Wait()

	fmt.Printf("\033[36m::\033[0m tests: \033[36m%d ran\033[0m, \033[32m%d passed\033[0m, \033[31m%d failed\033[0m", ran, passed, failed)

	if skipped > 0 {
		fmt.Printf(", \033[33m%d skipped\033[0m", skipped)
	}

	fmt.Println()

	if scanErr != nil {
		return scanErr
	}

	if err == nil {
		return nil
	}

	exit, ok := errors.AsType[*exec.ExitError](err)
	if ok {
		return &ExitError{code: exit.ExitCode()}
	}

	return err
}

func formatTestEvents(reader io.Reader, writer io.Writer) (int, int, int, int, error) {
	var (
		ran     int
		passed  int
		failed  int
		skipped int
	)

	scanner := bufio.NewScanner(reader)

	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		var event TestEvent

		err := json.Unmarshal(line, &event)
		if err != nil {
			fmt.Fprintln(writer, string(line))

			continue
		}

		elapsed := fmt.Sprintf("(%.2fs)", event.Elapsed)

		switch {
		case event.Test != "" && event.Action == "run":
			ran++

			fmt.Fprintf(writer, "   \033[90m-> run: \033[0m \033[36m%s\033[0m\n", event.Test)
		case event.Test != "" && event.Action == "pass":
			passed++

			fmt.Fprintf(writer, "   \033[32m-> pass:\033[0m \033[36m%s\033[0m \033[90m%s\033[0m\n", event.Test, elapsed)
		case event.Test != "" && event.Action == "fail":
			failed++

			fmt.Fprintf(writer, "   \033[31m-> fail:\033[0m \033[31;1m%s\033[0m \033[90m%s\033[0m\n", event.Test, elapsed)
		case event.Test != "" && event.Action == "skip":
			skipped++

			fmt.Fprintf(writer, "   \033[33m-> skip:\033[0m \033[36m%s\033[0m \033[90m%s\033[0m\n", event.Test, elapsed)
		case event.Test != "" && event.Action == "pause":
			fmt.Fprintf(writer, "   \033[90m-> pause:\033[0m \033[36m%s\033[0m\n", event.Test)
		case event.Test != "" && event.Action == "cont":
			fmt.Fprintf(writer, "   \033[90m-> cont: \033[0m \033[36m%s\033[0m\n", event.Test)
		case event.Test == "" && event.Action == "pass":
			fmt.Fprintf(writer, "\033[32m::\033[0m \033[32mok\033[0m     %s \033[90m%s\033[0m\n", event.Package, elapsed)
		case event.Test == "" && event.Action == "fail":
			fmt.Fprintf(writer, "\033[31m!!\033[0m \033[31;1mFAIL\033[0m   %s \033[90m%s\033[0m\n", event.Package, elapsed)
		case event.Test == "" && event.Action == "skip":
			fmt.Fprintf(writer, "\033[33m::\033[0m \033[33m?\033[0m      %s \033[90m[no test files]\033[0m\n", event.Package)
		case event.Action == "output" && !isGeneratedTestLine(event.Output):
			fmt.Fprint(writer, event.Output)
		}
	}

	return ran, passed, failed, skipped, scanner.Err()
}

func isGeneratedTestLine(output string) bool {
	trimmed := strings.TrimSpace(output)

	return strings.HasPrefix(trimmed, "=== RUN") || strings.HasPrefix(trimmed, "--- PASS:") ||
		strings.HasPrefix(trimmed, "--- FAIL:") || strings.HasPrefix(trimmed, "--- SKIP:") ||
		trimmed == "PASS" || trimmed == "FAIL" || strings.HasPrefix(trimmed, "ok\t") ||
		strings.HasPrefix(trimmed, "ok  \t") || strings.HasPrefix(trimmed, "?\t") ||
		strings.HasPrefix(trimmed, "?   \t") || strings.HasPrefix(trimmed, "FAIL\t")
}
