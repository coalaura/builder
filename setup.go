package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/urfave/cli/v3"
)

type SetupTool struct {
	Name       string
	Executable string
}

type SetupCommand struct {
	Name string
	Args []string
}

type setupInstallation struct {
	Tool    SetupTool
	Command SetupCommand
}

type SetupEnvironment struct {
	OS       string
	Root     bool
	Input    io.Reader
	Output   io.Writer
	LookPath func(string) (string, error)
	Run      func(context.Context, SetupCommand, io.Writer) error
}

var setupTools = [...]SetupTool{
	{Name: "Go", Executable: "go"},
	{Name: "Zig", Executable: "zig"},
}

func (e *SetupEnvironment) setup(ctx context.Context) error {
	missing := make([]SetupTool, 0, len(setupTools))

	for _, tool := range setupTools {
		installed, err := e.checkTool(ctx, tool)
		if err != nil {
			return err
		}

		if !installed {
			log.Infof("[setup] %s is not installed or is not in PATH\n", tool.Name)

			missing = append(missing, tool)
		}
	}

	if len(missing) == 0 {
		log.Infoln("[setup] Go and Zig are ready")

		return nil
	}

	installations := make([]setupInstallation, 0, len(missing))

	for _, tool := range missing {
		command, err := e.findInstaller(ctx, tool)
		if err != nil {
			return err
		}

		installations = append(installations, setupInstallation{Tool: tool, Command: command})
	}

	for _, installation := range installations {
		confirmed, err := confirmSetup(e.Input, installation)
		if err != nil {
			return err
		}

		if !confirmed {
			return fmt.Errorf("setup cancelled; %s was not installed", installation.Tool.Name)
		}

		log.Infof("[setup] installing %s\n", installation.Tool.Name)

		err = e.Run(ctx, installation.Command, e.Output)
		if err != nil {
			return fmt.Errorf("install %s: %w", installation.Tool.Name, err)
		}
	}

	for _, tool := range missing {
		installed, err := e.checkTool(ctx, tool)
		if err != nil {
			return err
		}

		if !installed {
			return fmt.Errorf("%s installation completed but %s is not in PATH; open a new terminal and run builder setup again", tool.Name, tool.Executable)
		}
	}

	log.Infoln("[setup] Go and Zig are ready")

	return nil
}

func (e *SetupEnvironment) checkTool(ctx context.Context, tool SetupTool) (bool, error) {
	path, err := e.LookPath(tool.Executable)
	if errors.Is(err, exec.ErrNotFound) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("find %s: %w", tool.Name, err)
	}

	var output bytes.Buffer

	command := SetupCommand{
		Name: path,
		Args: []string{"version"},
	}

	err = e.Run(ctx, command, &output)
	if err != nil {
		return false, fmt.Errorf("%s was found at %s but could not run: %w", tool.Name, path, err)
	}

	log.Infof("[setup] %s installed: %s\n", tool.Name, strings.TrimSpace(output.String()))

	return true, nil
}

func NewSetupSubcommand() *cli.Command {
	return &cli.Command{
		Name:      "setup",
		Usage:     "ensure Go and Zig are installed",
		ArgsUsage: " ",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 0 {
				return fmt.Errorf("setup does not accept arguments")
			}

			environment := newSetupEnvironment(cmd)

			return environment.setup(ctx)
		},
	}
}

func newSetupEnvironment(cmd *cli.Command) *SetupEnvironment {
	return &SetupEnvironment{
		OS:       runtime.GOOS,
		Root:     os.Geteuid() == 0,
		Input:    cmd.Reader,
		Output:   cmd.Writer,
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, command SetupCommand, output io.Writer) error {
			process := exec.CommandContext(ctx, command.Name, command.Args...)

			process.Stdin = cmd.Reader
			process.Stdout = output
			process.Stderr = output

			return process.Run()
		},
	}
}

func confirmSetup(input io.Reader, installation setupInstallation) (bool, error) {
	err := log.Infof("[setup] install %s using `%s`? [yN] ", installation.Tool.Name, formatCommand(installation.Command.Name, installation.Command.Args))
	if err != nil {
		return false, err
	}

	var (
		answer    strings.Builder
		character [1]byte
	)

	// Read only the confirmation line, leaving subsequent input for the package manager.
	for {
		_, err = io.ReadFull(input, character[:])
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return false, fmt.Errorf("read setup confirmation: %w", err)
		}

		if character[0] == '\n' {
			break
		}

		answer.WriteByte(character[0])
	}

	response := strings.TrimSpace(answer.String())

	return strings.EqualFold(response, "y") || strings.EqualFold(response, "yes"), nil
}
