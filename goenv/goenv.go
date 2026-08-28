// Package goenv prepares reproducible Go build settings.
package goenv

import (
	"runtime"
	"strings"
)

// Options describes the target and build characteristics.
type Options struct {
	Tags        []string
	Experiments []string
	OS          string
	Arch        string
	CGO         bool
	GUI         bool
	Optimize    bool
	Dynamic     bool
	Minify      bool
}

// Config contains environment overrides and command flags for a Go build.
type Config struct {
	BuildFlags []string
	LDFlags    string
	Mode       string
	Env        map[string]string
}

var zigTargets = map[string]string{
	"linux/amd64":   "x86_64-linux-musl",
	"linux/arm64":   "aarch64-linux-musl",
	"windows/amd64": "x86_64-windows-gnu",
	"windows/arm64": "aarch64-windows-gnu",
	"darwin/amd64":  "x86_64-macos-none",
	"darwin/arm64":  "aarch64-macos-none",
}

var cleanEnvironmentKeys = []string{
	"GO386",
	"GOAMD64",
	"GOARM",
	"GOARM64",
	"GOLOONG64",
	"GOMIPS",
	"GOMIPS64",
	"GOPPC64",
	"GORISCV64",
	"GOWASM",
	"GOFLAGS",
	"CC",
	"CXX",
	"CGO_CPPFLAGS",
	"CGO_CFLAGS",
	"CGO_CXXFLAGS",
	"CGO_FFLAGS",
	"CGO_LDFLAGS",
}

// Prepare returns all environment overrides and flags needed for a build.
// An empty environment value means that an inherited variable must be removed.
func Prepare(options Options) Config {
	targetOS := options.OS
	if targetOS == "" {
		targetOS = runtime.GOOS
	}

	arch := options.Arch
	if arch == "" {
		arch = runtime.GOARCH
	}

	env := make(map[string]string, len(cleanEnvironmentKeys)+5)

	for _, key := range cleanEnvironmentKeys {
		env[key] = ""
	}

	env["GOENV"] = "off"
	env["GOOS"] = targetOS
	env["GOARCH"] = arch

	buildFlags := []string{"-trimpath", "-buildvcs=false"}

	tags := uniqueCSV(strings.Join(options.Tags, ","))

	ldflags := "-s -w"

	if options.GUI && targetOS == "windows" {
		ldflags += " -H windowsgui"
	}

	if options.CGO {
		env["CGO_ENABLED"] = "1"
	} else {
		env["CGO_ENABLED"] = "0"

		tags = uniqueCSV(strings.Join(tags, ","), "netgo,osusergo")
	}

	if len(tags) > 0 {
		buildFlags = append(buildFlags, "-tags", strings.Join(tags, ","))
	}

	if arch == "amd64" {
		if options.Optimize {
			env["GOAMD64"] = "v3"
		} else {
			env["GOAMD64"] = "v1"
		}
	}

	experiments := uniqueCSV(strings.Join(options.Experiments, ","), "runtimefreegc,newinliner")

	env["GOEXPERIMENT"] = strings.Join(experiments, ",")

	mode := "pure"

	if options.CGO {
		mode = "cgo"

		if options.Dynamic {
			mode += ",dyn"
		} else if targetOS == "linux" || targetOS == "windows" {
			mode += ",static"
		}
	}

	if options.Optimize {
		mode += ",opt"
	} else {
		mode += ",compat"
	}

	if options.Minify {
		mode += ",min"
	}

	if options.CGO {
		configureCGO(env, &ldflags, options, targetOS, arch)
	}

	return Config{
		Env:        env,
		BuildFlags: buildFlags,
		LDFlags:    ldflags,
		Mode:       mode,
	}
}

func configureCGO(env map[string]string, ldflags *string, options Options, targetOS, arch string) {
	if !options.Dynamic && (targetOS == "linux" || targetOS == "windows") {
		*ldflags += " -linkmode external -extldflags=-static"
	}

	env["CC"] = "zig cc"
	env["CXX"] = "zig c++"

	zigTarget := zigTargets[targetOS+"/"+arch]

	if zigTarget != "" {
		env["CC"] += " -target " + zigTarget
		env["CXX"] += " -target " + zigTarget
	}

	archFlag := ""

	if arch == "amd64" {
		archFlag = "-march=x86_64"

		if options.Optimize {
			archFlag = "-march=x86_64_v3"
		}

		env["CC"] += " " + archFlag
		env["CXX"] += " " + archFlag
	}

	optimizationFlag := "-O3"

	if options.Minify {
		optimizationFlag = "-Os"
	}

	cflags := "-g0 " + optimizationFlag + " -ffunction-sections -fdata-sections"

	if archFlag != "" {
		cflags += " " + archFlag
	}

	env["CGO_CFLAGS"] = cflags
	env["CGO_CXXFLAGS"] = cflags

	if targetOS == "darwin" {
		env["CGO_LDFLAGS"] = "-Wl,-dead_strip"
	} else {
		env["CGO_LDFLAGS"] = "-Wl,--gc-sections"
	}
}

func uniqueCSV(values ...string) []string {
	result := make([]string, 0)
	seen := make(map[string]bool)

	for _, value := range values {
		for item := range strings.SplitSeq(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" && !seen[item] {
				seen[item] = true

				result = append(result, item)
			}
		}

	}

	return result
}
