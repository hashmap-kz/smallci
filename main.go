package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	tea "github.com/charmbracelet/bubbletea"
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
				Action: runAction,
			},
			{
				Name:      "init",
				Usage:     "print a default config for a given template",
				ArgsUsage: "<template>",
				Commands: []*cli.Command{
					{
						Name:   "go",
						Usage:  "Go project (lint, test, build)",
						Action: initGoAction,
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

func runAction(_ context.Context, cmd *cli.Command) error {
	configPath := cmd.String("c")

	cfg, err := LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", configPath, err)
	}

	pipeline := NewPipeline(cfg)
	m := NewModel(pipeline)

	p := tea.NewProgram(m, tea.WithAltScreen())
	m.SetProgram(p)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run program: %w", err)
	}

	for _, job := range pipeline.Jobs {
		if job.Status == StatusFailed {
			os.Exit(1)
		}
	}

	return nil
}
