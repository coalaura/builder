package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func ExecuteBuild(req *Request) error {
	switch req.Language {
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

		name := getOutputName(req.Project, req.TargetOS == "windows")

		output := filepath.Join(req.Cwd, name)

		Infof("[go/%s/%s] building %s (mode: %s)", req.TargetOS, name, main, cfg.Mode)

		args := []string{"build"}

		args = append(args, cfg.BuildFlags...)
		args = append(args, "-ldflags", cfg.LDFlags)
		args = append(args, "-o", output)
		args = append(args, cfg.Extra...)

		if !hasBuildTarget(cfg.Extra) {
			args = append(args, main)
		}

		start := time.Now()

		err = RunProcess(req.Project, cfg.Env, "go", args...)
		if err != nil {
			return err
		}

		printDuration(start, "built")

		if req.Minify {
			_, err = exec.LookPath("upx")
			if err != nil {
				Infof("upx not found, skipping compression")

				return nil
			}

			Infof("[upx] compressing %s", name)

			start = time.Now()

			err = RunProcess(req.Cwd, nil, "upx", "--best", "--lzma", output)
			if err != nil {
				return err
			}

			printDuration(start, "compressed")
		}

		return nil
	case "js":
		if !doesFileExists(filepath.Join(req.Project, "package.json")) {
			return errors.New("no package.json found for node build")
		}

		script := findPackageJsonScript(req.Project, []string{"build", "prod"})
		if script == "" {
			return errors.New("no script found in package.json")
		}

		Infof("[bun/%s] building %s", script, req.Project)

		start := time.Now()

		args := []string{"run", script}

		args = append(args, req.Forward...)

		err := RunProcess(req.Project, nil, "bun", args...)
		if err != nil {
			return err
		}

		printDuration(start, "built")

		return nil
	}

	return fmt.Errorf("%s is not a recognized project", req.Project)
}

func hasBuildTarget(args []string) bool {
	takesValue := map[string]bool{
		"-C": true, "-asmflags": true, "-buildmode": true, "-compiler": true,
		"-covermode": true, "-coverpkg": true, "-gccgoflags": true,
		"-gcflags": true, "-installsuffix": true,
		"-ldflags": true, "-mod": true, "-modfile": true, "-overlay": true,
		"-p": true, "-pgo": true, "-pkgdir": true, "-tags": true,
		"-toolexec": true, "-o": true,
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if takesValue[arg] {
			i++

			continue
		}

		if !strings.HasPrefix(arg, "-") {
			return true
		}
	}

	return false
}
