package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type StepConfig struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

type JobConfig struct {
	Name  string       `yaml:"name"`
	Steps []StepConfig `yaml:"steps"`
}

type Config struct {
	Jobs []JobConfig `yaml:"jobs"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
