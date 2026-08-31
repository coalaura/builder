package signing

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/sassoftware/relic/v8/lib/fruit/csblob"
	"github.com/sassoftware/relic/v8/lib/fruit/machos"
)

const (
	darwinRequirementMagic      = 0xfade0c00
	darwinRequirementVersion    = 1
	darwinRequirementIdentifier = 2
	darwinRequirementAnchorHash = 4
	darwinRequirementAnd        = 6
	darwinRequirementRootSlot   = uint32(0xffffffff)
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

	root := verifiedChain[len(verifiedChain)-1]
	identifier := darwinSigningIdentifier(root)

	requirement := buildDarwinDesignatedRequirement(identifier, root)

	file, err := os.OpenFile(options.Path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open binary for signing: %w", err)
	}

	defer file.Close()

	params := &csblob.SignatureParams{
		HashFunc:        crypto.SHA256,
		SigningIdentity: identifier,
		Requirements:    requirement,
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

	err = verifyTimestampedSignature(signature.Signature, root)
	if err != nil {
		return fmt.Errorf("verify Darwin signature: %w", err)
	}

	return nil
}

func darwinSigningIdentifier(root *x509.Certificate) string {
	digest := sha256.Sum256(root.RawSubjectPublicKeyInfo)

	return "private-ca." + hex.EncodeToString(digest[:])
}

func buildDarwinDesignatedRequirement(identifier string, root *x509.Certificate) []byte {
	var body bytes.Buffer

	writeDarwinRequirementUint32(&body, darwinRequirementVersion)
	writeDarwinRequirementUint32(&body, darwinRequirementAnd)
	writeDarwinRequirementUint32(&body, darwinRequirementIdentifier)
	writeDarwinRequirementData(&body, []byte(identifier))
	writeDarwinRequirementUint32(&body, darwinRequirementAnchorHash)
	writeDarwinRequirementUint32(&body, darwinRequirementRootSlot)

	rootHash := sha1.Sum(root.Raw)

	writeDarwinRequirementData(&body, rootHash[:])

	requirement := make([]byte, 8, body.Len()+8)

	binary.BigEndian.PutUint32(requirement, darwinRequirementMagic)
	binary.BigEndian.PutUint32(requirement[4:], uint32(body.Len()+8))

	return append(requirement, body.Bytes()...)
}

func writeDarwinRequirementUint32(buffer *bytes.Buffer, value uint32) {
	var encoded [4]byte

	binary.BigEndian.PutUint32(encoded[:], value)

	_, _ = buffer.Write(encoded[:])
}

func writeDarwinRequirementData(buffer *bytes.Buffer, value []byte) {
	writeDarwinRequirementUint32(buffer, uint32(len(value)))

	_, _ = buffer.Write(value)

	for buffer.Len()%4 != 0 {
		_ = buffer.WriteByte(0)
	}
}
