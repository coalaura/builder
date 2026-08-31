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
builder build [language] [os] [options] [go flags...] [target]
builder run [language] [options] [go flags...] [target] [-- arguments...]
builder test [language] [options] [go flags...] [target] [-- arguments...]
builder bench [language] [options] [go flags...] [target] [-- arguments...]
builder sign binary --sign key-file [--sign-chain file-or-url]... [--passphrase value]
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

Common Go build and test flags can be passed directly before `--`. Builder merges `-ldflags` and `-tags` with its generated values and lets explicit Go flags override conflicting defaults.

The generation options are mutually exclusive.

### Signing

- `--sign`: sign with a combined PEM or PFX/P12 key file
- `--sign-chain`: add certificates from a local file or HTTPS URL; repeat for multiple sources
- `--passphrase`: supply the key passphrase without an interactive prompt

Builder supports Authenticode signatures for Windows, thin Mach-O signatures for Darwin and appended CMS/PKCS#7 signatures for Linux. All formats use DigiCert RFC 3161 timestamps. The standalone `builder sign` command detects the binary format automatically.

Signing chains are verified against the system trust store. Use one or more `--sign-chain` options to provide missing intermediate or root certificates. Encrypted keys prompt for their passphrase unless `--passphrase` is provided.

```sh
builder build go linux --sign certificate.pfx --sign-chain issuing.pem --sign-chain root.pem
builder sign example.exe --sign certificate.pfx --sign-chain https://example.com/issuing.pem --passphrase secret
```

### Execution

- `--debug`: print the commands that would run without executing them

```sh
builder build go --cgo --dyn --pkg ./cmd/example
builder build go windows --output example.exe
builder build go -tags integration -ldflags "-X main.version=dev"
builder run go --pkg ./cmd/example -- banner.png
builder test go --no-generate --debug
```
