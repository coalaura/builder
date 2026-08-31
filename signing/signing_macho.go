package signing

import (
	"context"
	"crypto"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sassoftware/relic/v8/lib/fruit/csblob"
	"github.com/sassoftware/relic/v8/lib/fruit/machos"
)

func signDarwinBinary(options Options, passphraseDuration *time.Duration) error {
	certificate, verifiedChain, promptDuration, err := prepareSigningCertificate(options.SigningKey, options.SigningChains, options.Passphrase)
	if err != nil {
		return err
	}

	if passphraseDuration != nil {
		*passphraseDuration = promptDuration
	}

	certificate.Timestamper = newDarwinTimestamper(options.UserAgent)

	file, err := os.OpenFile(options.Path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open binary for signing: %w", err)
	}

	defer file.Close()

	params := &csblob.SignatureParams{
		HashFunc:        crypto.SHA256,
		Flags:           csblob.FlagRuntime,
		SigningIdentity: filepath.Base(options.Path),
	}

	patch, _, err := machos.Sign(context.Background(), file, certificate, params)
	if err != nil {
		return fmt.Errorf("sign Darwin binary: %w", err)
	}

	err = patch.Apply(file, options.Path)
	if err != nil {
		return fmt.Errorf("write Darwin signature: %w", err)
	}

	signature, err := machos.Verify(file, nil, nil, false)
	if err != nil {
		return fmt.Errorf("verify Darwin signature: %w", err)
	}

	err = verifyTimestampedSignature(signature.Signature, verifiedChain[len(verifiedChain)-1])
	if err != nil {
		return fmt.Errorf("verify Darwin signature: %w", err)
	}

	return nil
}
