package signing

import (
	"context"
	"crypto"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/sassoftware/relic/v8/lib/authenticode"
)

func signWindowsBinary(options Options, passphraseDuration *time.Duration) error {
	certificate, verifiedChain, promptDuration, err := prepareSigningCertificate(options.SigningKey, options.SigningChains, options.Passphrase)
	if err != nil {
		return err
	}

	if passphraseDuration != nil {
		*passphraseDuration = promptDuration
	}

	certificate.Timestamper = newWindowsTimestamper(options.UserAgent)

	file, err := os.OpenFile(options.Path, os.O_RDWR, 0)
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

	err = patch.Apply(file, options.Path)
	if err != nil {
		return fmt.Errorf("write Windows signature: %w", err)
	}

	_, err = verifyWindowsBinary(options.Path, verifiedChain, nil)

	return err
}

func verifyWindowsBinary(path string, certificates, chain []*x509.Certificate) ([]*x509.Certificate, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("verify Windows signature: %w", err)
	}

	defer file.Close()

	signatures, err := authenticode.VerifyPE(file, false)
	if err != nil {
		return nil, fmt.Errorf("verify Windows signature: %w", err)
	}

	if len(signatures) != 1 {
		return nil, fmt.Errorf("verify Windows signature: found %d signatures", len(signatures))
	}

	verified, err := verifyTimestampedSignature(&signatures[0].TimestampedSignature, certificates, chain)
	if err != nil {
		return nil, fmt.Errorf("verify Windows signature: %w", err)
	}

	return verified, nil
}
