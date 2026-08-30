package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sassoftware/relic/v8/lib/authenticode"
	"github.com/sassoftware/relic/v8/lib/certloader"
	"github.com/sassoftware/relic/v8/lib/fruit/csblob"
	"github.com/sassoftware/relic/v8/lib/fruit/machos"
	"github.com/sassoftware/relic/v8/lib/passprompt"
	"github.com/sassoftware/relic/v8/lib/pkcs7"
	"github.com/sassoftware/relic/v8/lib/pkcs9"
)

const (
	digiCertTimestampURL = "http://timestamp.digicert.com"
	maxSigningChainSize  = 4 << 20
	maxTimestampSize     = 4 << 20
)

type EmptyFirstPasswordPrompt struct {
	prompted bool
	prompt   passprompt.PasswordGetter
}

type RFC3161Timestamper struct {
	client *http.Client
	url    string
}

var (
	signingTimestamper pkcs9.Timestamper = &RFC3161Timestamper{
		client: &http.Client{Timeout: 30 * time.Second},
		url:    digiCertTimestampURL,
	}

	signingChainClient = &http.Client{Timeout: 30 * time.Second}
)

func (prompt *EmptyFirstPasswordPrompt) GetPasswd(message string) (string, error) {
	if !prompt.prompted {
		prompt.prompted = true

		return "", nil
	}

	return prompt.prompt.GetPasswd(message)
}

func (timestamper *RFC3161Timestamper) Timestamp(ctx context.Context, request *pkcs9.Request) (*pkcs7.ContentInfoSignedData, error) {
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
	httpRequest.Header.Set("User-Agent", "builder/"+Version)

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

func SignWindowsBinary(path, keyPath, chainSource string) error {
	certificate, verifiedChain, err := prepareSigningCertificate(keyPath, chainSource)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open binary for signing: %w", err)
	}

	defer file.Close()

	digest, err := authenticode.DigestPE(file, crypto.SHA256, false)
	if err != nil {
		return fmt.Errorf("digest Windows binary: %w", err)
	}

	patch, _, err := digest.Sign(context.Background(), certificate, nil)
	if err != nil {
		return fmt.Errorf("sign Windows binary: %w", err)
	}

	err = patch.Apply(file, path)
	if err != nil {
		return fmt.Errorf("write Windows signature: %w", err)
	}

	signatures, err := authenticode.VerifyPE(file, false)
	if err != nil {
		return fmt.Errorf("verify Windows signature: %w", err)
	}

	if len(signatures) != 1 {
		return fmt.Errorf("verify Windows signature: found %d signatures", len(signatures))
	}

	roots := x509.NewCertPool()
	roots.AddCert(verifiedChain[len(verifiedChain)-1])

	signingTime := time.Now()

	if signatures[0].CounterSignature != nil {
		signingTime = signatures[0].CounterSignature.SigningTime
	}

	err = signatures[0].Signature.VerifyChain(roots, nil, x509.ExtKeyUsageCodeSigning, signingTime)
	if err != nil {
		return fmt.Errorf("verify embedded signing chain: %w", err)
	}

	return nil
}

func SignDarwinBinary(path, keyPath, chainSource string) error {
	certificate, verifiedChain, err := prepareSigningCertificate(keyPath, chainSource)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open binary for signing: %w", err)
	}

	defer file.Close()

	params := &csblob.SignatureParams{
		HashFunc:        crypto.SHA256,
		Flags:           csblob.FlagRuntime,
		SigningIdentity: filepath.Base(path),
	}

	patch, _, err := machos.Sign(context.Background(), file, certificate, params)
	if err != nil {
		return fmt.Errorf("sign Darwin binary: %w", err)
	}

	err = patch.Apply(file, path)
	if err != nil {
		return fmt.Errorf("write Darwin signature: %w", err)
	}

	signature, err := machos.Verify(file, nil, nil, false)
	if err != nil {
		return fmt.Errorf("verify Darwin signature: %w", err)
	}

	roots := x509.NewCertPool()

	roots.AddCert(verifiedChain[len(verifiedChain)-1])

	signingTime := time.Now()

	if signature.Signature.CounterSignature != nil {
		signingTime = signature.Signature.CounterSignature.SigningTime
	}

	err = signature.Signature.Signature.VerifyChain(roots, nil, x509.ExtKeyUsageCodeSigning, signingTime)
	if err != nil {
		return fmt.Errorf("verify embedded signing chain: %w", err)
	}

	return nil
}

func prepareSigningCertificate(keyPath, chainSource string) (*certloader.Certificate, []*x509.Certificate, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read signing key: %w", err)
	}

	certificate, err := loadSigningCertificate(keyData, passprompt.PasswordPrompt{})
	if err != nil {
		return nil, nil, fmt.Errorf("load signing key: %w", err)
	}

	certificate.Timestamper = signingTimestamper

	chainCertificates, err := loadSigningChain(context.Background(), chainSource, signingChainClient)
	if err != nil {
		return nil, nil, fmt.Errorf("load signing chain: %w", err)
	}

	verifiedChain, err := verifySigningChain(certificate, chainCertificates, time.Now())
	if err != nil {
		return nil, nil, fmt.Errorf("verify signing chain: %w", err)
	}

	certificate.Certificates = verifiedChain

	return certificate, verifiedChain, nil
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

func verifySigningChain(certificate *certloader.Certificate, supplied []*x509.Certificate, currentTime time.Time) ([]*x509.Certificate, error) {
	if certificate.Leaf == nil {
		return nil, fmt.Errorf("signing certificate is missing")
	}

	candidates := append([]*x509.Certificate(nil), certificate.Certificates...)

	candidates = append(candidates, supplied...)
	candidates = uniqueCertificates(candidates)

	chain := []*x509.Certificate{certificate.Leaf}

	used := map[string]bool{
		string(certificate.Leaf.Raw): true,
	}

	current := certificate.Leaf

	for {
		if bytes.Equal(current.RawIssuer, current.RawSubject) {
			err := current.CheckSignature(current.SignatureAlgorithm, current.RawTBSCertificate, current.Signature)
			if err != nil {
				return nil, fmt.Errorf("self-signed certificate %q has an invalid signature: %w", current.Subject.String(), err)
			}

			break
		}

		issuers := make([]*x509.Certificate, 0, 1)

		for _, candidate := range candidates {
			if used[string(candidate.Raw)] || !bytes.Equal(current.RawIssuer, candidate.RawSubject) {
				continue
			}

			err := current.CheckSignatureFrom(candidate)
			if err == nil {
				issuers = append(issuers, candidate)
			}
		}

		if len(issuers) == 0 {
			if current == certificate.Leaf {
				return nil, fmt.Errorf("issuer for %q is missing", current.Subject.String())
			}

			break
		}

		if len(issuers) > 1 {
			return nil, fmt.Errorf("multiple issuers match %q", current.Subject.String())
		}

		current = issuers[0]

		chain = append(chain, current)
		used[string(current.Raw)] = true
	}

	for _, candidate := range candidates {
		if !used[string(candidate.Raw)] {
			return nil, fmt.Errorf("certificate %q is not part of the signing chain", candidate.Subject.String())
		}
	}

	roots := x509.NewCertPool()

	roots.AddCert(chain[len(chain)-1])

	intermediates := x509.NewCertPool()

	if len(chain) > 2 {
		for _, intermediate := range chain[1 : len(chain)-1] {
			intermediates.AddCert(intermediate)
		}
	}

	options := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   currentTime,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}

	_, err := certificate.Leaf.Verify(options)
	if err != nil {
		return nil, err
	}

	return chain, nil
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

	pkcs12Prompt := &EmptyFirstPasswordPrompt{prompt: prompt}

	return certloader.ParsePKCS12(trimmed, pkcs12Prompt)
}
