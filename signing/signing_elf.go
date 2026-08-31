package signing

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/sassoftware/relic/v8/lib/pkcs7"
	"github.com/sassoftware/relic/v8/lib/pkcs9"
)

const (
	linuxModuleSignatureMetadataSize = 12
	linuxModuleSignatureTypePKCS7    = 2
	linuxModuleSignatureMarker       = "~Module signature appended~\n"
)

func signLinuxBinary(options Options, passphraseDuration *time.Duration) error {
	certificate, verifiedChain, promptDuration, err := prepareSigningCertificate(options.SigningKey, options.SigningChains, options.Passphrase)
	if err != nil {
		return err
	}

	if passphraseDuration != nil {
		*passphraseDuration = promptDuration
	}

	contents, err := os.ReadFile(options.Path)
	if err != nil {
		return fmt.Errorf("read Linux binary for signing: %w", err)
	}

	err = validateELF(contents)
	if err != nil {
		return err
	}

	digest := crypto.SHA256.New()

	_, _ = digest.Write(contents)

	builder := pkcs7.NewBuilder(certificate.Signer(), certificate.Chain(), crypto.SHA256)

	err = builder.SetDetachedContent(pkcs7.OidData, digest.Sum(nil))
	if err != nil {
		return fmt.Errorf("prepare Linux signature: %w", err)
	}

	signedData, err := builder.Sign()
	if err != nil {
		return fmt.Errorf("sign Linux binary: %w", err)
	}

	timestamper := newLinuxTimestamper(options.UserAgent)

	err = timestampLinuxSignature(context.Background(), signedData, timestamper)
	if err != nil {
		return fmt.Errorf("timestamp Linux signature: %w", err)
	}

	signature, err := signedData.Marshal()
	if err != nil {
		return fmt.Errorf("encode Linux signature: %w", err)
	}

	if len(signature) > math.MaxUint32 {
		return fmt.Errorf("linux signature exceeds %d bytes", uint64(math.MaxUint32))
	}

	metadata := make([]byte, linuxModuleSignatureMetadataSize)

	metadata[2] = linuxModuleSignatureTypePKCS7

	binary.BigEndian.PutUint32(metadata[8:], uint32(len(signature)))

	trailer := make([]byte, 0, len(signature)+len(metadata)+len(linuxModuleSignatureMarker))

	trailer = append(trailer, signature...)
	trailer = append(trailer, metadata...)
	trailer = append(trailer, linuxModuleSignatureMarker...)

	file, err := os.OpenFile(options.Path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("open Linux binary for signing: %w", err)
	}

	_, writeErr := file.Write(trailer)
	closeErr := file.Close()

	if writeErr != nil {
		return fmt.Errorf("write Linux signature: %w", writeErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close signed Linux binary: %w", closeErr)
	}

	err = verifyLinuxBinary(options.Path, verifiedChain[len(verifiedChain)-1])
	if err != nil {
		return fmt.Errorf("verify Linux signature: %w", err)
	}

	return nil
}

func timestampLinuxSignature(ctx context.Context, signedData *pkcs7.ContentInfoSignedData, timestamper pkcs9.Timestamper) error {
	if len(signedData.Content.SignerInfos) != 1 {
		return fmt.Errorf("CMS signature has %d signers", len(signedData.Content.SignerInfos))
	}

	signerInfo := &signedData.Content.SignerInfos[0]

	request := &pkcs9.Request{
		EncryptedDigest: signerInfo.EncryptedDigest,
		Hash:            crypto.SHA256,
	}

	token, err := timestamper.Timestamp(ctx, request)
	if err != nil {
		return err
	}

	err = pkcs9.AddStampToSignedData(signerInfo, *token)
	if err != nil {
		return fmt.Errorf("attach RFC 3161 timestamp: %w", err)
	}

	return nil
}

func verifyLinuxBinary(path string, signingRoot *x509.Certificate) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read signed Linux binary: %w", err)
	}

	unsigned, signature, err := parseLinuxModuleSignature(contents)
	if err != nil {
		return err
	}

	signedData, err := pkcs7.Unmarshal(signature)
	if err != nil {
		return fmt.Errorf("parse CMS signature: %w", err)
	}

	if !signedData.Content.ContentInfo.ContentType.Equal(pkcs7.OidData) {
		return fmt.Errorf("cms signature has unexpected content type %s", signedData.Content.ContentInfo.ContentType.String())
	}

	embedded, err := signedData.Content.ContentInfo.Bytes()
	if err != nil {
		return fmt.Errorf("parse CMS content: %w", err)
	}

	if embedded != nil {
		return fmt.Errorf("cms signature is not detached")
	}

	verified, err := signedData.Content.Verify(unsigned, false)
	if err != nil {
		return fmt.Errorf("verify CMS signature: %w", err)
	}

	timestamped, err := pkcs9.VerifyOptionalTimestamp(verified)
	if err != nil {
		return fmt.Errorf("verify RFC 3161 timestamp: %w", err)
	}

	err = verifyTimestampedSignature(&timestamped, signingRoot)
	if err != nil {
		return err
	}

	return nil
}

func parseLinuxModuleSignature(contents []byte) ([]byte, []byte, error) {
	minimumSize := len(linuxModuleSignatureMarker) + linuxModuleSignatureMetadataSize
	if len(contents) <= minimumSize {
		return nil, nil, fmt.Errorf("linux module signature trailer is missing")
	}

	markerOffset := len(contents) - len(linuxModuleSignatureMarker)
	if !bytes.Equal(contents[markerOffset:], []byte(linuxModuleSignatureMarker)) {
		return nil, nil, fmt.Errorf("linux module signature marker is missing")
	}

	metadataOffset := markerOffset - linuxModuleSignatureMetadataSize
	metadata := contents[metadataOffset:markerOffset]

	if metadata[2] != linuxModuleSignatureTypePKCS7 {
		return nil, nil, fmt.Errorf("linux signature is not PKCS#7")
	}

	if metadata[0] != 0 || metadata[1] != 0 || metadata[3] != 0 || metadata[4] != 0 ||
		metadata[5] != 0 || metadata[6] != 0 || metadata[7] != 0 {
		return nil, nil, fmt.Errorf("linux PKCS#7 signature metadata has unexpected non-zero fields")
	}

	signatureSize := uint64(binary.BigEndian.Uint32(metadata[8:]))
	if signatureSize == 0 || signatureSize >= uint64(metadataOffset) {
		return nil, nil, fmt.Errorf("linux PKCS#7 signature length is invalid")
	}

	signatureOffset := metadataOffset - int(signatureSize)
	unsigned := contents[:signatureOffset]

	err := validateELF(unsigned)
	if err != nil {
		return nil, nil, err
	}

	return unsigned, contents[signatureOffset:metadataOffset], nil
}

func validateELF(contents []byte) error {
	if len(contents) < 4 || !bytes.Equal(contents[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		return fmt.Errorf("binary is not an ELF file")
	}

	return nil
}
