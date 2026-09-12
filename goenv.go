package main

import (
	"os"
	"strings"

	"github.com/coalaura/builder/goenv"
)

func prepareGo(req *Request) goenv.Config {
	return goenv.Prepare(goenv.Options{
		CGO:         req.CGO,
		OS:          req.TargetOS,
		Arch:        req.TargetArch,
		GUI:         req.GUI,
		Optimize:    !req.Compatible,
		Dynamic:     req.Dynamic,
		Minify:      req.Minify,
		Experiments: strings.Split(os.Getenv("GOEXPERIMENT"), ","),
	})
}
