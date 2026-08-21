package main

import (
	"fmt"
	"path/filepath"
)

func ExecuteRun(req *Request) error {
	switch req.Language {
	case "php":
		if !doesFileExists(filepath.Join(req.Project, "artisan")) {
			return fmt.Errorf("%s is not a recognized php project", req.Project)
		}

		Infof("[php] running artisan serve")

		args := []string{"artisan", "serve", "--port=80"}

		args = append(args, req.Forward...)

		return RunProcess(req.Project, nil, "php", args...)
	case "go":
		main := req.RunTarget
		if main == "" {
			main = findGoMain(req.Project)
		}

		err := GenerateGo(req.Project)
		if err != nil {
			return err
		}

		cfg := PrepareGo(req)

		Infof("[go] running %s (mode: %s)", main, cfg.Mode)

		args := []string{"run"}

		args = append(args, resolveTagArgs(cfg.BuildFlags)...)
		args = append(args, main)
		args = append(args, cfg.Extra...)

		return RunProcess(req.Project, cfg.Env, "go", args...)
	case "js":
		if req.RunTarget != "" && doesFileExists(req.RunTarget) {
			Infof("[bun] running %s", req.RunTarget)

			args := []string{req.RunTarget}

			args = append(args, req.Forward...)

			return RunProcess(req.Project, nil, "bun", args...)
		}

		script := findPackageJsonScript(req.Project, []string{"dev", "watch", "start", "test"})
		if script != "" {
			Infof("[bun/%s] running %s", script, req.Project)

			args := []string{"run", script}

			args = append(args, req.Forward...)

			return RunProcess(req.Project, nil, "bun", args...)
		}

		file := findFirstExistingFile(req.Project, []string{"index.js", "main.js", "app.js"})
		if file != "" {
			Infof("[bun/%s] running %s", file, req.Project)

			args := []string{file}

			args = append(args, req.Forward...)

			return RunProcess(req.Project, nil, "bun", args...)
		}

		return fmt.Errorf("%s is not a recognized js project", req.Project)
	}

	return fmt.Errorf("%s is not a recognized project", req.Project)
}
