package main

import "fmt"

func ExecuteBench(req *Request) error {
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

		Infof("[go] benchmarking %s (mode: %s)", target, cfg.Mode)

		args := []string{"test", "-run=^$", "-bench=.", "-benchmem"}

		args = append(args, resolveTagArgs(cfg.BuildFlags)...)
		args = append(args, target)
		args = append(args, req.Forward...)

		return RunProcess(req.Debug, req.Project, cfg.Env, "go", args...)
	case "js":
		if req.RunTarget != "" && doesFileExists(req.RunTarget) {
			Infof("[bun] benchmarking %s", req.RunTarget)

			return RunProcess(req.Debug, req.Project, nil, "bun", append([]string{req.RunTarget}, req.Forward...)...)
		}

		script := findPackageJsonScript(req.Project, []string{"bench", "benchmark"})
		if script != "" {
			Infof("[bun/%s] benchmarking %s", script, req.Project)

			return RunProcess(req.Debug, req.Project, nil, "bun", append([]string{"run", script}, req.Forward...)...)
		}

		file := findFirstExistingFile(req.Project, []string{"bench.js", "bench.ts", "benchmark.js", "benchmark.ts"})
		if file != "" {
			Infof("[bun/%s] benchmarking %s", file, req.Project)

			return RunProcess(req.Debug, req.Project, nil, "bun", append([]string{file}, req.Forward...)...)
		}

		return fmt.Errorf("%s is not a recognized js bench project", req.Project)
	}

	return fmt.Errorf("%s is not a recognized benchmark project", req.Project)
}
