# builder

A small (opinionated) CLI for building, running, testing and benchmarking Go and JavaScript projects.

The Go build environment is also available as an importable package:

```go
import "github.com/coalaura/builder/goenv"

config := goenv.Prepare(goenv.Options{
	CGO:      true,
	OS:       "linux",
	Arch:     "amd64",
	Optimize: true,
})

// config.Env contains the complete overrides for the selected build.
// Empty values remove inherited variables.
```

## Install

```sh
go install .
```

## Usage

```sh
builder build [language] [os] [options] [target]
builder run [language] [options] [target] [-- arguments...]
builder test [language] [options] [target] [-- arguments...]
builder bench [language] [options] [target] [-- arguments...]
```

Builder attempts to detect the project language when omitted. Only `build` accepts an operating system target.

### Options

- `--pure`: disable CGO
- `--dyn`, `--dynamic`: dynamically link CGO builds
- `--compat`, `--compatible`: favor CPU compatibility
- `--opt`, `--optimize`: explicitly select the default optimized build mode
- `--min`, `--minify`: minimize and compress builds
- `--gui`: use the Windows GUI subsystem for Go builds and runs
- `--package`, `--pkg`: select a Go package
- `--no-generate`: skip the default `go generate ./...` step
- `--output`: override the Go build output name or path
- `--debug`: print the commands that would run without executing them

```sh
builder build go --dyn --pkg ./cmd/example
builder build go windows --output example.exe
builder run go --pkg ./cmd/example -- banner.png
builder test go --no-generate --debug
```
