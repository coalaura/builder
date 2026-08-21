package main

import (
	"os"
	"runtime"
	"strings"
)

type GoConfig struct {
	Env        map[string]string
	BuildFlags []string
	LDFlags    string
	Extra      []string
	Mode       string
}

var ZigTargets = map[string]string{
	"linux/amd64":   "x86_64-linux-musl",
	"linux/arm64":   "aarch64-linux-musl",
	"windows/amd64": "x86_64-windows-gnu",
	"windows/arm64": "aarch64-windows-gnu",
	"darwin/amd64":  "x86_64-macos-none",
	"darwin/arm64":  "aarch64-macos-none",
}

func PrepareGo(req *Request) GoConfig {
	tags := make([]string, 0)
	seenTags := make(map[string]bool)

	addTags := func(value string) {
		for tag := range strings.SplitSeq(value, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" && !seenTags[tag] {
				seenTags[tag] = true

				tags = append(tags, tag)
			}
		}
	}

	env := map[string]string{
		"GOOS":   req.TargetOS,
		"GOARCH": runtime.GOARCH,
	}

	buildFlags := []string{"-trimpath", "-buildvcs=false"}

	ldflags := "-s -w"

	if req.Pure {
		env["CGO_ENABLED"] = "0"
		env["CC"] = ""
		env["CXX"] = ""
		env["CGO_CFLAGS"] = ""
		env["CGO_CXXFLAGS"] = ""
		env["CGO_LDFLAGS"] = ""

		addTags("netgo,osusergo")
	} else {
		env["CGO_ENABLED"] = "1"
	}

	if len(tags) > 0 {
		buildFlags = append(buildFlags, "-tags", strings.Join(tags, ","))
	}

	if runtime.GOARCH == "amd64" {
		if req.Compatible {
			env["GOAMD64"] = "v1"
		} else {
			env["GOAMD64"] = "v3"
		}
	}

	experiments := uniqueCSV(os.Getenv("GOEXPERIMENT"), "runtimefreegc,newinliner")

	env["GOEXPERIMENT"] = strings.Join(experiments, ",")

	mode := "cgo"

	if req.Pure {
		mode = "pure"
	} else if req.Dynamic {
		mode += ",dyn"
	} else if req.TargetOS == "linux" || req.TargetOS == "windows" {
		mode += ",static"
	}

	if req.Compatible {
		mode += ",compat"
	} else {
		mode += ",opt"
	}

	if req.Minify {
		mode += ",min"
	}

	if !req.Pure {
		if !req.Dynamic && (req.TargetOS == "linux" || req.TargetOS == "windows") {
			ldflags += " -linkmode external -extldflags=-static"
		}

		zigTarget := getZigTarget(req.TargetOS, runtime.GOARCH)

		env["CC"] = "zig cc"
		env["CXX"] = "zig c++"

		if zigTarget != "" {
			env["CC"] += " -target " + zigTarget
			env["CXX"] += " -target " + zigTarget
		}

		archFlag := ""

		if runtime.GOARCH == "amd64" {
			archFlag = "-march=x86_64_v3"

			if req.Compatible {
				archFlag = "-march=x86_64"
			}

			env["CC"] += " " + archFlag
			env["CXX"] += " " + archFlag
		}

		opt := "-O3"

		if req.Minify {
			opt = "-Os"
		}

		cflags := "-g0 " + opt + " -ffunction-sections -fdata-sections"

		if archFlag != "" {
			cflags += " " + archFlag
		}

		env["CGO_CFLAGS"] = cflags
		env["CGO_CXXFLAGS"] = cflags

		if req.TargetOS == "darwin" {
			env["CGO_LDFLAGS"] = "-Wl,-dead_strip"
		} else {
			env["CGO_LDFLAGS"] = "-Wl,--gc-sections"
		}
	}

	return GoConfig{
		Env:        env,
		BuildFlags: buildFlags,
		LDFlags:    ldflags,
		Extra:      append([]string(nil), req.Forward...),
		Mode:       mode,
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

func getZigTarget(targetOS, arch string) string {
	return ZigTargets[targetOS+"/"+arch]
}
