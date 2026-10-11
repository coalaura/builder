package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/coalaura/builder/goenv"
)

func prepareGo(req *Request) goenv.Config {
	isRace := goBoolFlagEnabled(req.GoFlags, "race")

	if isRace && !req.CGO {
		log.Warnf("[%s] enabling cgo for -race\n", req.GoExecutable())

		req.CGO = true
	}

	return goenv.Prepare(goenv.Options{
		CGO:             req.CGO,
		OS:              req.TargetOS,
		Arch:            req.TargetArch,
		Libc:            req.Libc,
		Race:            isRace,
		GUI:             req.GUI,
		Optimize:        !req.Compatible,
		Dynamic:         req.Dynamic,
		Minify:          req.Minify,
		MetadataEntries: len(req.Metadata),
		Cwd:             req.Cwd,
		Experiments:     strings.Split(os.Getenv("GOEXPERIMENT"), ","),
	})
}

func goBoolFlagEnabled(args []string, name string) bool {
	var enabled bool

	for _, flag := range parseGoFlags(args) {
		if flag.Name != name {
			continue
		}

		if !flag.Joined {
			enabled = true

			continue
		}

		value, err := strconv.ParseBool(flag.Value)
		if err != nil {
			continue
		}

		enabled = value
	}

	return enabled
}
