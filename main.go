package main

import (
	"context"
	"errors"
	"os"

	"github.com/urfave/cli/v3"
)

var Version = "dev"

func main() {
	err := NewCLI().Run(context.Background(), os.Args)
	if err == nil {
		return
	}

	if status, ok := errors.AsType[cli.ExitCoder](err); ok {
		if status.Error() != "" {
			Errorf("%s", status.Error())
		}

		os.Exit(status.ExitCode())
	}

	Errorf("%s", err)

	os.Exit(1)
}

func NewCLI() *cli.Command {
	return &cli.Command{
		Name:           "builder",
		Version:        Version,
		Usage:          "build, run, test and benchmark projects",
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Commands: []*cli.Command{
			NewSubcommand("build", "build a project", []string{"go", "js"}, true),
			NewSubcommand("run", "run a project", []string{"go", "js", "php"}, false),
			NewSubcommand("test", "test a project", []string{"go", "js"}, false),
			NewSubcommand("bench", "benchmark a project", []string{"go", "js"}, false),
		},
	}
}

func NewSubcommand(name, usage string, languages []string, allowOS bool) *cli.Command {
	return &cli.Command{
		Name:            name,
		Usage:           usage,
		ArgsUsage:       "[language] [os] [--pure] [--compat] [--opt] [--min] [target] [-- go arguments...]",
		SkipFlagParsing: true,
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
				return cli.ShowSubcommandHelp(cmd)
			}

			req, err := parseRequest(name, args, languages, allowOS)
			if err != nil {
				return err
			}

			return ExecuteRequest(req)
		},
	}
}
