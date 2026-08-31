package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

type SignRequest struct {
	Binary       string
	SigningKey   string
	SigningChain string
	Passphrase   string
}

func NewSignSubcommand() *cli.Command {
	return &cli.Command{
		Name:            "sign",
		Usage:           "sign an existing binary",
		ArgsUsage:       "binary --sign key-file [--sign-chain file-or-url] [--passphrase value]",
		SkipFlagParsing: true,
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
				return cli.ShowSubcommandHelp(cmd)
			}

			req, err := parseSignRequest(args)
			if err != nil {
				return err
			}

			return ExecuteSign(req)
		},
	}
}

func ExecuteSign(req *SignRequest) error {
	Infof("[sign] signing %s", filepath.Base(req.Binary))

	start := time.Now()
	passphraseDuration := time.Duration(0)

	err := SignBinary(req.Binary, req.SigningKey, req.SigningChain, req.Passphrase, &passphraseDuration)
	if err != nil {
		return err
	}

	printDuration(start.Add(passphraseDuration), "signed")

	return nil
}

func parseSignRequest(args []string) (*SignRequest, error) {
	req := &SignRequest{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		lower := strings.ToLower(arg)

		switch lower {
		case "--sign":
			value, next, err := parseSignOptionValue(args, i, lower)
			if err != nil {
				return nil, err
			}

			req.SigningKey = value
			i = next
		case "--sign-chain":
			value, next, err := parseSignOptionValue(args, i, lower)
			if err != nil {
				return nil, err
			}

			req.SigningChain = value
			i = next
		case "--passphrase":
			value, next, err := parseSignOptionValue(args, i, lower)
			if err != nil {
				return nil, err
			}

			req.Passphrase = value
			i = next
		default:
			equals := strings.IndexByte(lower, '=')
			if equals >= 0 {
				name := lower[:equals]
				value := arg[equals+1:]

				switch name {
				case "--sign":
					req.SigningKey = value
				case "--sign-chain":
					req.SigningChain = value
				case "--passphrase":
					req.Passphrase = value
				default:
					return nil, fmt.Errorf("unknown argument for sign: %s", arg)
				}

				if value == "" {
					return nil, fmt.Errorf("%s requires a value", name)
				}

				continue
			}

			if strings.HasPrefix(arg, "-") || req.Binary != "" {
				return nil, fmt.Errorf("unknown argument for sign: %s", arg)
			}

			req.Binary = arg
		}
	}

	if req.Binary == "" {
		return nil, fmt.Errorf("sign requires a binary")
	}

	if req.SigningKey == "" {
		return nil, fmt.Errorf("sign requires --sign")
	}

	return req, nil
}

func parseSignOptionValue(args []string, index int, option string) (string, int, error) {
	if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
		return "", index, fmt.Errorf("%s requires a value", option)
	}

	return args[index+1], index + 1, nil
}
