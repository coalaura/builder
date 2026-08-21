# builder

A small CLI for building, running, testing and benchmarking Go and JavaScript projects.

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

Builder attempts to detect the project language when omitted.

### Options

- `--pure`: disable CGO
- `--dyn`, `--dynamic`: dynamically link CGO builds
- `--compat`, `--compatible`: favor CPU compatibility
- `--min`, `--minify`: minimize and compress builds
- `--package`, `--pkg`: select a Go package

```sh
builder build go --dyn --pkg ./cmd/example
builder run go --pkg ./cmd/example -- banner.png
```
