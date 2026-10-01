package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

const setupQueryTimeout = 30 * time.Second

type SetupPackageManager struct {
	OS          string
	Name        string
	QueryName   string
	GoPackage   string
	ZigPackage  string
	QueryArgs   []string
	InstallArgs []string
	NeedsRoot   bool
}

var setupPackageManagers = [...]SetupPackageManager{
	{
		OS:          "windows",
		Name:        "winget",
		GoPackage:   "GoLang.Go",
		ZigPackage:  "zig.zig",
		QueryArgs:   []string{"show", "--exact", "--accept-source-agreements", "--disable-interactivity", "--id"},
		InstallArgs: []string{"install", "--exact", "--accept-source-agreements", "--accept-package-agreements", "--disable-interactivity", "--id"},
	},
	{
		OS:          "windows",
		Name:        "choco",
		GoPackage:   "golang",
		ZigPackage:  "zig",
		QueryArgs:   []string{"search", "--exact", "--limit-output"},
		InstallArgs: []string{"install", "--yes", "--no-progress"},
	},
	{
		OS:          "darwin",
		Name:        "brew",
		GoPackage:   "go",
		ZigPackage:  "zig",
		QueryArgs:   []string{"info", "--formula"},
		InstallArgs: []string{"install", "--formula"},
	},
	{
		OS:          "linux",
		Name:        "apt-get",
		QueryName:   "apt-cache",
		GoPackage:   "golang-go",
		ZigPackage:  "zig",
		QueryArgs:   []string{"show", "--no-all-versions"},
		InstallArgs: []string{"install", "--yes"},
		NeedsRoot:   true,
	},
	{
		OS:          "linux",
		Name:        "dnf",
		GoPackage:   "golang",
		ZigPackage:  "zig",
		QueryArgs:   []string{"info", "--available"},
		InstallArgs: []string{"install", "--assumeyes"},
		NeedsRoot:   true,
	},
	{
		OS:          "linux",
		Name:        "pacman",
		GoPackage:   "go",
		ZigPackage:  "zig",
		QueryArgs:   []string{"-Si"},
		InstallArgs: []string{"-S", "--needed", "--noconfirm"},
		NeedsRoot:   true,
	},
	{
		OS:          "linux",
		Name:        "apk",
		GoPackage:   "go",
		ZigPackage:  "zig",
		QueryArgs:   []string{"search", "--exact"},
		InstallArgs: []string{"add"},
		NeedsRoot:   true,
	},
	{
		OS:          "linux",
		Name:        "brew",
		GoPackage:   "go",
		ZigPackage:  "zig",
		QueryArgs:   []string{"info", "--formula"},
		InstallArgs: []string{"install", "--formula"},
	},
}

func (e *SetupEnvironment) findInstaller(ctx context.Context, tool SetupTool) (SetupCommand, error) {
	for index := range setupPackageManagers {
		manager := &setupPackageManagers[index]
		if manager.OS != e.OS {
			continue
		}

		path, err := e.LookPath(manager.Name)
		if err != nil {
			continue
		}

		queryPath := path

		if manager.QueryName != "" {
			queryPath, err = e.LookPath(manager.QueryName)
			if err != nil {
				continue
			}
		}

		packageName := manager.GoPackage

		if tool.Executable == "zig" {
			packageName = manager.ZigPackage
		}

		command := SetupCommand{
			Name: path,
			Args: appendSetupPackage(manager.InstallArgs, packageName),
		}

		if manager.NeedsRoot && !e.Root {
			sudo, err := e.LookPath("sudo")
			if err != nil {
				continue
			}

			arguments := make([]string, 0, len(command.Args)+1)

			arguments = append(arguments, command.Name)
			arguments = append(arguments, command.Args...)

			command.Name = sudo
			command.Args = arguments
		}

		query := SetupCommand{
			Name: queryPath,
			Args: appendSetupPackage(manager.QueryArgs, packageName),
		}

		var output bytes.Buffer

		queryContext, cancel := context.WithTimeout(ctx, setupQueryTimeout)

		err = e.Run(queryContext, query, &output)
		cancel()

		if ctx.Err() != nil {
			return SetupCommand{}, ctx.Err()
		}

		if err != nil || !setupPackageAvailable(manager.Name, packageName, output.String()) {
			log.Infof("[setup] %s could not find an available %s package\n", manager.Name, tool.Name)

			continue
		}

		return command, nil
	}

	return SetupCommand{}, fmt.Errorf("no usable package manager found to install %s on %s; a supported manager must be in PATH, offer the package and have installation privileges (root or sudo on Linux)", tool.Name, e.OS)
}

func appendSetupPackage(arguments []string, packageName string) []string {
	result := make([]string, 0, len(arguments)+1)

	result = append(result, arguments...)
	result = append(result, packageName)

	return result
}

func setupPackageAvailable(manager, packageName, output string) bool {
	if manager != "choco" && manager != "apk" && manager != "apt-get" {
		return strings.TrimSpace(output) != ""
	}

	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)

		switch manager {
		case "choco":
			name, _, found := strings.Cut(line, "|")
			if found && strings.EqualFold(name, packageName) {
				return true
			}
		case "apk":
			if strings.HasPrefix(line, packageName+"-") {
				return true
			}
		case "apt-get":
			if line == "Package: "+packageName {
				return true
			}
		}
	}

	return false
}
