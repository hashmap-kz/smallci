# smallci

A minimal local CI runner with a live terminal UI.

**Jobs run in parallel. Steps within a job run sequentially.**

[![License](https://img.shields.io/github/license/hashmap-kz/smallci)](https://github.com/hashmap-kz/smallci/blob/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/hashmap-kz/smallci)](https://goreportcard.com/report/github.com/hashmap-kz/smallci)
[![Go Reference](https://pkg.go.dev/badge/github.com/hashmap-kz/smallci.svg)](https://pkg.go.dev/github.com/hashmap-kz/smallci)
[![Workflow Status](https://img.shields.io/github/actions/workflow/status/hashmap-kz/smallci/ci.yml?branch=master)](https://github.com/hashmap-kz/smallci/actions/workflows/ci.yml?query=branch:master)
[![Latest Release](https://img.shields.io/github/v/release/hashmap-kz/smallci)](https://github.com/hashmap-kz/smallci/releases/latest)

![Preview](https://raw.githubusercontent.com/hashmap-kz/assets/main/smallci/08-demo.gif)

_Purpose: concurrency, grouping, aggressive simplicity._

---

## Install

Using Go:

```bash
go install github.com/hashmap-kz/smallci@latest
```

Using Homebrew:

```bash
brew tap hashmap-kz/homebrew-tap
brew install smallci
```

---

## Usage

```bash
# Run with smallci.yaml in current directory
smallci

# Specify a config file
smallci run -c path/to/config.yaml

# Generate a default config and save it
smallci init go > smallci.yaml
```

---

## Config

```yaml
jobs:
  - name: lint
    steps:
      - name: fmt
        run: gofumpt -w .
      - name: vet
        run: go vet ./...

  - name: test
    steps:
      - name: unit
        run: go test -v -race ./...

  - name: build
    steps:
      - name: build
        run: go build -ldflags="-s -w" ./...
        env:
          CGO_ENABLED: "0"
          GOOS: linux
```

---

## Keybindings

| Key                    | Action                                                 |
|------------------------|--------------------------------------------------------|
| `↑` / `↓` or `k` / `j` | Navigate one row                                       |
| `J` / `K`              | Jump to next / previous job                            |
| `h` / `l`              | Fold job / unfold job or enter its steps               |
| `enter` / `space`      | Toggle fold on selected job                            |
| `tab`                  | Switch focus (tree <-> logs)                           |
| `f`                    | Jump to first failure                                  |
| `r`                    | Re-run selected job; re-run selected step if on a step |
| `R`                    | Reload all — re-run the full pipeline from scratch     |
| `t`                    | Toggle timeline view (shows per-step bars)             |
| `z`                    | Toggle full-width log (hide / show tree)               |
| `/`                    | Search in logs (type query, `Enter` to confirm)        |
| `n` / `N`              | Next / previous search match                           |
| `C`                    | Cycle color theme (13 built-in themes)                 |
| `H`                    | Help                                                   |
| `ctrl+c`               | Quit                                                   |

---

## License

MIT. See [LICENSE](./LICENSE) for details.
