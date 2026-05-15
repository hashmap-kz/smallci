package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hashmap-kz/smallci/internal/cmd"
	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:           "smallci",
		Usage:          "lightweight local CI runner",
		DefaultCommand: "run",
		Commands: []*cli.Command{
			{
				Name:  "run",
				Usage: "run the CI pipeline",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "c",
						Value:   "smallci.yaml",
						Usage:   "config file path",
						Sources: cli.EnvVars("SMALLCI_CONFIG"),
					},
				},
				Action: cmd.RunAction,
			},
			{
				Name:      "init",
				Usage:     "print a default config for a given template",
				ArgsUsage: "<template>",
				Commands: []*cli.Command{
					{
						Name:   "go",
						Usage:  "Go project (lint, test, build)",
						Action: cmd.InitGoAction,
					},
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
