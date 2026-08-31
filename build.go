package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/coalaura/builder/signing"
)

func ExecuteBuild(req *Request) error {
	switch req.Language {
	case "go":
		main := req.RunTarget
		if main == "" {
			main = findGoMain(req.Project, req.Debug)
		}

		err := GenerateGo(req)
		if err != nil {
			return err
		}

		cfg := prepareGo(req)

		name := req.Output
		if name == "" {
			name = getOutputName(req.Project, req.TargetOS == "windows")
		}

		output := name
		if !filepath.IsAbs(output) {
			output = filepath.Join(req.Cwd, output)
		}

		Infof("[go/%s/%s] building %s (mode: %s)", req.TargetOS, filepath.Base(output), main, cfg.Mode)

		args := []string{"build"}

		args = append(args, resolveGoFlags(req, cfg)...)
		args = append(args, "-o", output)
		args = append(args, req.Forward...)
		args = append(args, main)

		start := time.Now()

		err = RunProcess(req.Debug, req.Project, cfg.Env, "go", args...)
		if err != nil {
			return err
		}

		if !req.Debug {
			printDuration(start, "built")
		}

		if req.Minify {
			upxAvailable := true

			if !req.Debug {
				_, err = exec.LookPath("upx")
				if err != nil {
					Infof("upx not found, skipping compression")
					upxAvailable = false
				}
			}

			if upxAvailable {
				Infof("[upx] compressing %s", filepath.Base(output))

				start = time.Now()

				err = RunProcess(req.Debug, req.Cwd, nil, "upx", "--best", "--lzma", output)
				if err != nil {
					return err
				}

				if !req.Debug {
					printDuration(start, "compressed")
				}
			}
		}

		if req.SigningKey != "" {
			Infof("[sign] signing %s", filepath.Base(output))

			if req.Debug {
				return nil
			}

			start = time.Now()

			passphraseDuration, err := signing.Sign(signing.Options{
				Path:          output,
				SigningKey:    req.SigningKey,
				SigningChains: req.SigningChains,
				Passphrase:    req.Passphrase,
				UserAgent:     "builder/" + Version,
			})

			if err != nil {
				return err
			}

			printDuration(start.Add(passphraseDuration), "signed")
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

		err := RunProcess(req.Debug, req.Project, nil, "bun", args...)
		if err != nil {
			return err
		}

		if !req.Debug {
			printDuration(start, "built")
		}

		return nil
	}

	return fmt.Errorf("%s is not a recognized project", req.Project)
}
