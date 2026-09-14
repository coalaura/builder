# builder

A small (opinionated) CLI for building, running, testing and benchmarking Go and JavaScript projects.

Fittingly, builder builds and signs its own releases in CI, much like the Go compiler compiling itself.

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
builder verify binary [--cert file-or-url] [--cert-chain file-or-url]...
```

Builder attempts to detect the project language when omitted. Only `build` accepts an operating system target and the `--arch` architecture option.

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

When cross-compiling a Darwin CGO build, builder uses an existing macOS SDK when one is available. It checks a valid `SDKROOT` first, then xmac and OSXCross layouts under the invocation directory, SDK/SDKs directories beside `bin` entries in `PATH`, xmac and OSXCross layouts under the home directory and finally common `/usr/local/osxcross` and `/opt/osxcross` prefixes on Linux. SDK discovery is optional; builder does not download an SDK and native macOS discovery remains Zig's responsibility.

### Project options

- `--gui`: use the Windows GUI subsystem for Go builds and runs
- `--pkg`, `--package`: select a Go package
- `--gen`, `--generate`: explicitly run `go generate ./...` (the default)
- `--no-gen`, `--no-generate`: skip `go generate ./...`
- `--out`, `--output`: override the Go build output name or path
- `--arch`: override the Go target architecture (defaults to the host architecture)

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

### Verifying

- `--cert`: require the signing certificate to match a certificate from a local file or HTTPS URL
- `--cert-chain`: add certificates from a local file or HTTPS URL; repeat for multiple sources

The standalone `builder verify` command detects the binary format and checks for a valid RFC 3161 timestamp. With `--cert`, the signing certificate must match the supplied certificate. Without `--cert`, the signature is verified against the system trust store. Missing intermediate or root certificates can be provided with `--cert-chain`. The verified certificate chain, along with each certificate's trust level and the timestamp, is printed on success.

```sh
builder verify example.exe
builder verify example.exe --cert certificate.pem --cert-chain https://example.com/root.pem
```

### Execution

- `--debug`: print the commands that would run without executing them

```sh
builder build go --cgo --dyn --pkg ./cmd/example
builder build go linux --arch arm64
builder build go windows --output example.exe
builder build go -tags integration -ldflags "-X main.version=dev"
builder run go --pkg ./cmd/example -- banner.png
builder test go --no-generate --debug
```
