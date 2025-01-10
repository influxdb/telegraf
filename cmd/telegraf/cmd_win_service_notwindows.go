//go:build !windows

package main

import (
	"io"

	"github.com/urfave/cli/v2"
)

func cliFlags() []cli.Flag {
	return make([]cli.Flag, 0)
}

func getServiceCommands(io.Writer) []*cli.Command {
	return nil
}
