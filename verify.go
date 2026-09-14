package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/coalaura/builder/signing"
	"github.com/urfave/cli/v3"
)

type VerifyRequest struct {
	Binary            string
	Certificate       string
	CertificateChains []string
}

func NewVerifySubcommand() *cli.Command {
	return &cli.Command{
		Name:            "verify",
		Usage:           "verify a signed binary",
		ArgsUsage:       "binary [--cert file-or-url] [--cert-chain file-or-url]...",
		SkipFlagParsing: true,
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
				return cli.ShowSubcommandHelp(cmd)
			}

			req, err := parseVerifyRequest(args)
			if err != nil {
				return err
			}

			return ExecuteVerify(req)
		},
	}
}

func ExecuteVerify(req *VerifyRequest) error {
	log.Infof("[verify] verifying %s\n", filepath.Base(req.Binary))

	start := time.Now()

	verification, err := signing.Verify(signing.VerifyOptions{
		Path:              req.Binary,
		Certificate:       req.Certificate,
		CertificateChains: req.CertificateChains,
	})

	if err != nil {
		return err
	}

	printCertificateChain(verification)
	printTimestamp(verification.Timestamp, verification.TimestampAuthority)
	printDuration(start, "verified")

	return nil
}

func printCertificateChain(verification *signing.Verification) {
	width := len("intermediate")

	for index, certificate := range verification.Chain {
		role := "intermediate"

		switch index {
		case 0:
			role = "leaf"
		case len(verification.Chain) - 1:
			role = "root"
		}

		log.Subf("%-*s  %s %s\n", width, role, certificate.Subject.String(), trustBadge(verification.Trust[index]))
	}
}

func trustBadge(trust string) string {
	color := "\033[90m"

	switch trust {
	case signing.TrustSystem:
		color = "\033[32m"
	case signing.TrustSelfSigned:
		color = "\033[33m"
	case signing.TrustUntrusted:
		color = "\033[31m"
	}

	return color + "(" + trust + ")\033[90m"
}

func printTimestamp(timestamp time.Time, authority *x509.Certificate) {
	width := len("intermediate")

	formatted := timestamp.UTC().Format("2006-01-02 15:04:05 MST")

	if authority == nil {
		log.Subf("%-*s  %s\n", width, "timestamp", formatted)

		return
	}

	log.Subf("%-*s  %s by %s\n", width, "timestamp", formatted, authority.Subject.String())
}

func parseVerifyRequest(args []string) (*VerifyRequest, error) {
	req := &VerifyRequest{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		lower := strings.ToLower(arg)

		switch lower {
		case "--cert":
			value, next, err := parseSignOptionValue(args, i, lower)
			if err != nil {
				return nil, err
			}

			req.Certificate = value

			i = next
		case "--cert-chain":
			value, next, err := parseSignOptionValue(args, i, lower)
			if err != nil {
				return nil, err
			}

			req.CertificateChains = append(req.CertificateChains, value)

			i = next
		default:
			equals := strings.IndexByte(lower, '=')
			if equals >= 0 {
				name := lower[:equals]
				value := arg[equals+1:]

				switch name {
				case "--cert":
					req.Certificate = value
				case "--cert-chain":
					req.CertificateChains = append(req.CertificateChains, value)
				default:
					return nil, fmt.Errorf("unknown argument for verify: %s", arg)
				}

				if value == "" {
					return nil, fmt.Errorf("%s requires a value", name)
				}

				continue
			}

			if strings.HasPrefix(arg, "-") || req.Binary != "" {
				return nil, fmt.Errorf("unknown argument for verify: %s", arg)
			}

			req.Binary = arg
		}
	}

	if req.Binary == "" {
		return nil, fmt.Errorf("verify requires a binary")
	}

	return req, nil
}
