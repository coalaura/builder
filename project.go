package main

import (
	"bufio"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type PackageJson struct {
	Scripts map[string]string `json:"scripts"`
}

func findPackageJsonScript(project string, allowed []string) string {
	body, err := os.ReadFile(filepath.Join(project, "package.json"))
	if err != nil {
		return ""
	}

	var pkg PackageJson

	err = json.Unmarshal(body, &pkg)
	if err != nil {
		return ""
	}

	for _, name := range allowed {
		if pkg.Scripts[name] != "" {
			return name
		}
	}

	return ""
}

func findGoMain(project string, debug bool) string {
	if doesDirectoryHaveGoMain(project) {
		return project
	}

	if debug {
		Infof("[debug] %s", formatCommand("go", []string{"list", "-f", "{{.Name}}|{{.Dir}}", "./..."}))

		return project
	}

	cmd := exec.Command("go", "list", "-f", "{{.Name}}|{{.Dir}}", "./...")

	cmd.Dir = project

	output, err := cmd.Output()
	if err != nil {
		return project
	}

	candidates := make([]string, 0)

	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	for scanner.Scan() {
		name, dir, ok := strings.Cut(scanner.Text(), "|")
		if ok && name == "main" && doesDirectoryHaveGoMain(dir) {
			candidates = append(candidates, dir)
		}
	}

	if len(candidates) == 0 {
		return project
	}

	sort.Slice(candidates, func(i, j int) bool {
		left := getPathDepth(candidates[i])
		right := getPathDepth(candidates[j])

		if left == right {
			return candidates[i] < candidates[j]
		}

		return left < right
	})

	return candidates[0]
}

func doesDirectoryHaveGoMain(dir string) bool {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return false
	}

	for _, name := range files {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil || file.Name.Name != "main" {
			continue
		}

		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Name.Name == "main" {
				return true
			}
		}
	}

	return false
}

func getPathDepth(path string) int {
	return strings.Count(filepath.Clean(path), string(filepath.Separator))
}

func getOutputName(project string, windows bool) string {
	base := filepath.Base(project)

	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}

	base = strings.NewReplacer("-", "_", ".", "_").Replace(base)

	base = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}

		return -1
	}, base)

	if base == "" {
		base = "app"
	}

	if windows {
		base += ".exe"
	}

	return base
}
