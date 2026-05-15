package main

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/urfave/cli/v3"
)

//go:embed templates/go.yaml
var goTemplate []byte

func initGoAction(_ context.Context, _ *cli.Command) error {
	fmt.Print(string(goTemplate))
	return nil
}
