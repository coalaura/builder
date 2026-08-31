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
- `--sign`: sign a Windows, Darwin or Linux Go build with a combined PEM or PFX/P12 key file
- `--sign-chain`: add certificates from a local file or HTTPS URL; repeat for multiple sources
- `--passphrase`: supply the signing-key passphrase without an interactive prompt

The generation options are mutually exclusive. Signing uses embedded Authenticode support for Windows, thin Mach-O support for Darwin and a CMS/PKCS#7 module-style appended signature for Linux. The standalone command detects the binary format. Encrypted keys prompt for their passphrase without echoing it unless `--passphrase` supplies it programmatically. Windows and Darwin signatures are timestamped through DigiCert. The signing key must contain a verifiable certificate chain unless `--sign-chain` supplies the missing certificates. Self-signed roots are used for validation; Mach-O signatures include the root certificate as required by Apple, while Linux signatures embed the leaf and intermediate certificates.

### Execution

- `--debug`: print the commands that would run without executing them

```sh
builder build go --cgo --dyn --pkg ./cmd/example
builder build go windows --output example.exe
builder build go windows --sign certificate.pfx
builder build go windows --sign certificate.pfx --sign-chain https://example.com/issuing.pem
builder build go darwin --sign certificate.p12 --sign-chain issuing.pem
builder build go linux --sign certificate.pem --sign-chain issuing.pem
builder sign example.exe --sign certificate.pfx --sign-chain https://example.com/issuing.pem --passphrase secret
builder run go --pkg ./cmd/example -- banner.png
builder test go --no-generate --debug
```
