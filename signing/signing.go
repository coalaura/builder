// Package signing signs and verifies PE, Mach-O, and ELF binaries.
package signing

import (
	"bytes"
	"context"
	"crypto/x509"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sassoftware/relic/v8/lib/certloader"
	"github.com/sassoftware/relic/v8/lib/passprompt"
	"github.com/sassoftware/relic/v8/lib/pkcs7"
	"github.com/sassoftware/relic/v8/lib/pkcs9"
)

const (
	digiCertTimestampURL = "http://timestamp.digicert.com"
	appleTimestampURL    = "http://timestamp.apple.com/ts01"
	maxSigningChainSize  = 4 << 20
	maxTimestampSize     = 4 << 20
)

const (
	binaryFormatUnknown = iota
	binaryFormatWindows
	binaryFormatDarwin
	binaryFormatLinux
)

type emptyFirstPasswordPrompt struct {
	prompted bool
	prompt   passprompt.PasswordGetter
}

type timedPasswordPrompt struct {
	prompt   passprompt.PasswordGetter
	duration time.Duration
}

type suppliedPasswordPrompt struct {
	password string
	used     bool
}

type rfc3161Timestamper struct {
	client    *http.Client
	url       string
	userAgent string
}

// Options describes a binary signing operation.
type Options struct {
	Path          string
	SigningKey    string
	SigningChains []string
	Passphrase    string
	UserAgent     string
}

var (
	timestampClient    = &http.Client{Timeout: 30 * time.Second}
	signingChainClient = &http.Client{Timeout: 30 * time.Second}
)

func (prompt *emptyFirstPasswordPrompt) GetPasswd(message string) (string, error) {
	if !prompt.prompted {
		prompt.prompted = true

		return "", nil
	}

	return prompt.prompt.GetPasswd(message)
}

func (prompt *timedPasswordPrompt) GetPasswd(message string) (string, error) {
	start := time.Now()

	password, err := prompt.prompt.GetPasswd(message)

	prompt.duration += time.Since(start)

	return password, err
}

func (prompt *suppliedPasswordPrompt) GetPasswd(string) (string, error) {
	if prompt.used {
		return "", fmt.Errorf("supplied passphrase was rejected")
	}

	prompt.used = true

	return prompt.password, nil
}

func (timestamper *rfc3161Timestamper) Timestamp(ctx context.Context, request *pkcs9.Request) (*pkcs7.ContentInfoSignedData, error) {
	if !request.Hash.Available() {
		return nil, fmt.Errorf("timestamp hash is unavailable")
	}

	digest := request.Hash.New()

	_, _ = digest.Write(request.EncryptedDigest)

	timestampRequest, httpRequest, err := pkcs9.NewRequest(timestamper.url, request.Hash, digest.Sum(nil))
	if err != nil {
		return nil, err
	}

	httpRequest.Header.Set("Accept", "application/timestamp-reply")
	httpRequest.Header.Set("User-Agent", timestamper.userAgent)

	response, err := timestamper.client.Do(httpRequest.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("contact timestamp server: %w", err)
	}

	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxTimestampSize+1))
	if err != nil {
		return nil, fmt.Errorf("read timestamp response: %w", err)
	}

	if len(body) > maxTimestampSize {
		return nil, fmt.Errorf("timestamp response exceeds %d bytes", maxTimestampSize)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("timestamp server returned %s", response.Status)
	}

	token, err := timestampRequest.ParseResponse(body)
	if err != nil {
		return nil, fmt.Errorf("parse timestamp response: %w", err)
	}

	return token, nil
}

// Sign detects the binary format, signs the file, and verifies the result.
// It returns the time spent waiting for an interactive passphrase.
func Sign(options Options) (time.Duration, error) {
	format, err := detectBinaryFormat(options.Path)
	if err != nil {
		return 0, err
	}

	passphraseDuration := time.Duration(0)

	switch format {
	case binaryFormatWindows:
		err = signWindowsBinary(options, &passphraseDuration)
	case binaryFormatDarwin:
		err = signDarwinBinary(options, &passphraseDuration)
	case binaryFormatLinux:
		err = signLinuxBinary(options, &passphraseDuration)
	default:
		err = fmt.Errorf("binary format is not supported")
	}

	return passphraseDuration, err
}

func newWindowsTimestamper(userAgent string) pkcs9.Timestamper {
	return &rfc3161Timestamper{
		client:    timestampClient,
		url:       digiCertTimestampURL,
		userAgent: userAgent,
	}
}

func newDarwinTimestamper(userAgent string) pkcs9.Timestamper {
	return &rfc3161Timestamper{
		client:    timestampClient,
		url:       appleTimestampURL,
		userAgent: userAgent,
	}
}

func prepareSigningCertificate(keyPath string, chainSources []string, passphrase string) (*certloader.Certificate, []*x509.Certificate, time.Duration, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("read signing key: %w", err)
	}

	interactivePrompt := &timedPasswordPrompt{prompt: passprompt.PasswordPrompt{}}
	var prompt passprompt.PasswordGetter = interactivePrompt

	if passphrase != "" {
		prompt = &suppliedPasswordPrompt{password: passphrase}
	}

	certificate, err := loadSigningCertificate(keyData, prompt)
	if err != nil {
		return nil, nil, interactivePrompt.duration, fmt.Errorf("load signing key: %w", err)
	}

	chainCertificates, err := loadSigningChains(context.Background(), chainSources, signingChainClient)
	if err != nil {
		return nil, nil, interactivePrompt.duration, fmt.Errorf("load signing chain: %w", err)
	}

	verifiedChain, err := verifySigningChain(certificate, chainCertificates, time.Now())
	if err != nil {
		return nil, nil, interactivePrompt.duration, fmt.Errorf("verify signing chain: %w", err)
	}

	certificate.Certificates = verifiedChain

	return certificate, verifiedChain, interactivePrompt.duration, nil
}

func verifyTimestampedSignature(signature *pkcs9.TimestampedSignature, signingRoot *x509.Certificate) error {
	if signature.CounterSignature == nil {
		return fmt.Errorf("secure timestamp is missing")
	}

	err := signature.CounterSignature.VerifyChain(nil, nil)
	if err != nil {
		return fmt.Errorf("verify timestamp chain: %w", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(signingRoot)

	err = signature.Signature.VerifyChain(roots, nil, x509.ExtKeyUsageCodeSigning, signature.CounterSignature.SigningTime)
	if err != nil {
		return fmt.Errorf("verify embedded signing chain: %w", err)
	}

	return nil
}

func detectBinaryFormat(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return binaryFormatUnknown, fmt.Errorf("open binary for format detection: %w", err)
	}

	defer file.Close()

	var header [4]byte

	_, err = io.ReadFull(file, header[:])
	if err != nil {
		return binaryFormatUnknown, fmt.Errorf("read binary format: %w", err)
	}

	if header[0] == 'M' && header[1] == 'Z' {
		return binaryFormatWindows, nil
	}

	if bytes.Equal(header[:], []byte{0x7f, 'E', 'L', 'F'}) {
		return binaryFormatLinux, nil
	}

	bigEndianMagic := binary.BigEndian.Uint32(header[:])
	littleEndianMagic := binary.LittleEndian.Uint32(header[:])
	machMagic := macho.Magic32 &^ 1

	if bigEndianMagic&^1 == machMagic || littleEndianMagic&^1 == machMagic {
		return binaryFormatDarwin, nil
	}

	return binaryFormatUnknown, fmt.Errorf("binary format is not supported")
}

func loadSigningChain(ctx context.Context, source string, client *http.Client) ([]*x509.Certificate, error) {
	if source == "" {
		return nil, nil
	}

	var data []byte

	parsedURL, err := url.Parse(source)
	if err != nil {
		return nil, err
	}

	if parsedURL.Scheme != "" && filepath.VolumeName(source) == "" {
		if !strings.EqualFold(parsedURL.Scheme, "https") || parsedURL.Host == "" {
			return nil, fmt.Errorf("URL must use HTTPS")
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, err
		}

		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", source, err)
		}

		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("download %s: server returned %s", source, response.Status)
		}

		if response.Request.URL.Scheme != "https" {
			return nil, fmt.Errorf("download %s: redirect must use HTTPS", source)
		}

		data, err = io.ReadAll(io.LimitReader(response.Body, maxSigningChainSize+1))
		if err != nil {
			return nil, err
		}
	} else {
		data, err = os.ReadFile(source)
		if err != nil {
			return nil, err
		}
	}

	if len(data) > maxSigningChainSize {
		return nil, fmt.Errorf("certificate bundle exceeds %d bytes", maxSigningChainSize)
	}

	certificates, err := certloader.ParseX509Certificates(data)
	if err != nil {
		return nil, err
	}

	return certificates, nil
}

func loadSigningChains(ctx context.Context, sources []string, client *http.Client) ([]*x509.Certificate, error) {
	var certificates []*x509.Certificate

	for _, source := range sources {
		loaded, err := loadSigningChain(ctx, source, client)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}

		certificates = append(certificates, loaded...)
	}

	return certificates, nil
}

func verifySigningChain(certificate *certloader.Certificate, supplied []*x509.Certificate, currentTime time.Time) ([]*x509.Certificate, error) {
	if certificate.Leaf == nil {
		return nil, fmt.Errorf("signing certificate is missing")
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}

	return verifySigningChainWithRoots(certificate, supplied, currentTime, roots)
}

func verifySigningChainWithRoots(certificate *certloader.Certificate, supplied []*x509.Certificate, currentTime time.Time, roots *x509.CertPool) ([]*x509.Certificate, error) {
	candidates := append([]*x509.Certificate(nil), certificate.Certificates...)

	candidates = append(candidates, supplied...)
	candidates = uniqueCertificates(candidates)

	if roots == nil {
		roots = x509.NewCertPool()
	} else {
		roots = roots.Clone()
	}

	intermediates := x509.NewCertPool()

	for _, candidate := range candidates {
		if bytes.Equal(candidate.RawIssuer, candidate.RawSubject) {
			err := candidate.CheckSignature(candidate.SignatureAlgorithm, candidate.RawTBSCertificate, candidate.Signature)
			if err != nil {
				return nil, fmt.Errorf("self-signed certificate %q has an invalid signature: %w", candidate.Subject.String(), err)
			}

			roots.AddCert(candidate)

			continue
		}

		intermediates.AddCert(candidate)
	}

	options := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   currentTime,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}

	chains, err := certificate.Leaf.Verify(options)
	if err != nil {
		return nil, err
	}

	for _, chain := range chains {
		if chainContainsAll(chain, candidates) {
			return chain, nil
		}
	}

	for _, candidate := range candidates {
		if !chainContainsCertificate(chains[0], candidate) {
			return nil, fmt.Errorf("certificate %q is not part of the signing chain", candidate.Subject.String())
		}
	}

	return nil, fmt.Errorf("signing chain is invalid")
}

func chainContainsAll(chain, certificates []*x509.Certificate) bool {
	for _, certificate := range certificates {
		if !chainContainsCertificate(chain, certificate) {
			return false
		}
	}

	return true
}

func chainContainsCertificate(chain []*x509.Certificate, certificate *x509.Certificate) bool {
	for _, member := range chain {
		if bytes.Equal(member.Raw, certificate.Raw) {
			return true
		}
	}

	return false
}

func uniqueCertificates(certificates []*x509.Certificate) []*x509.Certificate {
	unique := make([]*x509.Certificate, 0, len(certificates))
	seen := make(map[string]bool, len(certificates))

	for _, certificate := range certificates {
		if certificate == nil || seen[string(certificate.Raw)] {
			continue
		}

		seen[string(certificate.Raw)] = true
		unique = append(unique, certificate)
	}

	return unique
}

func loadSigningCertificate(keyData []byte, prompt passprompt.PasswordGetter) (*certloader.Certificate, error) {
	trimmed := bytes.TrimSpace(keyData)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("key file is empty")
	}

	if bytes.HasPrefix(trimmed, []byte("-----BEGIN")) {
		key, err := certloader.ParseAnyPrivateKey(trimmed, prompt)
		if err != nil {
			return nil, err
		}

		return certloader.LoadTokenCertificates(key, "", "", trimmed)
	}

	pkcs12Prompt := &emptyFirstPasswordPrompt{prompt: prompt}

	return certloader.ParsePKCS12(trimmed, pkcs12Prompt)
}
