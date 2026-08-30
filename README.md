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

### Build modes

- `--cgo`: enable CGO
- `--pure`: disable CGO (the default)
- `--dyn`, `--dynamic`: dynamically link CGO builds
- `--stat`, `--static`: statically link CGO builds (the default)
- `--compat`, `--compatible`: favor CPU compatibility and disable optimized mode
- `--opt`, `--optimize`: favor optimization and disable compatibility mode (the default)
- `--min`, `--minify`: minimize and compress builds
- `--no-min`, `--no-minify`: disable minification

Opposing build modes are mutually exclusive. Combining `--cgo` with `--pure`, `--dyn` with `--stat`, `--compat` with `--opt` or `--min` with `--no-min` is an error.

### Project options

- `--gui`: use the Windows GUI subsystem for Go builds and runs
- `--pkg`, `--package`: select a Go package
- `--gen`, `--generate`: explicitly run `go generate ./...` (the default)
- `--no-gen`, `--no-generate`: skip `go generate ./...`
- `--out`, `--output`: override the Go build output name or path
- `--sign`: sign a Windows or Darwin Go build with a combined PEM or PFX/P12 key file
- `--sign-chain`: add intermediate certificates from a local file or HTTPS URL

The generation options are mutually exclusive. Signing uses embedded Authenticode support for Windows and Mach-O support for Darwin. Encrypted keys prompt for their passphrase without echoing it, and signatures are timestamped through DigiCert. The signing key must contain a verifiable certificate chain unless `--sign-chain` supplies the missing intermediates. Self-signed roots are used for validation; Mach-O signatures include the root certificate as required by Apple.

### Execution

- `--debug`: print the commands that would run without executing them

```sh
builder build go --cgo --dyn --pkg ./cmd/example
builder build go windows --output example.exe
builder build go windows --sign certificate.pfx
builder build go windows --sign certificate.pfx --sign-chain https://example.com/issuing.pem
builder build go darwin --sign certificate.p12 --sign-chain issuing.pem
builder run go --pkg ./cmd/example -- banner.png
builder test go --no-generate --debug
```
