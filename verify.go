package main

import (
	"context"
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
	Infof("[verify] verifying %s", filepath.Base(req.Binary))

	start := time.Now()

	err := signing.Verify(signing.VerifyOptions{
		Path:              req.Binary,
		Certificate:       req.Certificate,
		CertificateChains: req.CertificateChains,
	})

	if err != nil {
		return err
	}

	printDuration(start, "verified")

	return nil
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
