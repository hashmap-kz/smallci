package cmd

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hashmap-kz/smallci/internal/config"
	"github.com/hashmap-kz/smallci/internal/pipeline"
	"github.com/hashmap-kz/smallci/internal/tui"
	"github.com/urfave/cli/v3"
)

func RunAction(_ context.Context, cmd *cli.Command) error {
	configPath := cmd.String("c")

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", configPath, err)
	}

	newPipeline := pipeline.NewPipeline(cfg)
	m := tui.NewModel(newPipeline)

	p := tea.NewProgram(m, tea.WithAltScreen())
	m.SetProgram(p)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run program: %w", err)
	}

	for _, job := range newPipeline.Jobs {
		if job.Status == pipeline.StatusFailed {
			os.Exit(1)
		}
	}

	return nil
}
