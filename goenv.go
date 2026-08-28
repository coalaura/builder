package main

import (
	"os"
	"runtime"
	"strings"

	"github.com/coalaura/builder/goenv"
)

func prepareGo(req *Request) goenv.Config {
	return goenv.Prepare(goenv.Options{
		CGO:         req.CGO,
		OS:          req.TargetOS,
		Arch:        runtime.GOARCH,
		GUI:         req.GUI,
		Optimize:    !req.Compatible,
		Dynamic:     req.Dynamic,
		Minify:      req.Minify,
		Experiments: strings.Split(os.Getenv("GOEXPERIMENT"), ","),
	})
}
