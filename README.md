# smallci

A minimal local CI runner with a live terminal UI. Groups run in **parallel**; steps within a group run **sequentially**.

```
┌ Pipeline ───────────────────────────────────────────────┐
│   ✓ fmt              0.18s                              │
│   ✓ vet              1.41s                              │
│ ▶ ⠹ unit-tests       4.82s                              │
│   · race-tests       waiting · group: test              │
│   · build            waiting · group: build             │
└─────────────────────────────────────────────────────────┘

┌ logs · unit-tests ──────────────────────────────────────┐
│ $ go test ./...                                         │
│ ok   ./internal/config       0.214s                     │
│ ok   ./internal/storage      0.921s                     │
└─────────────────────────────────────────────────────────┘

  q quit · ↑/↓ select · enter expand · f failed-only
```

## Install

```bash
go install github.com/hashmap-kz/smallci@latest
```

Or build from source:

```bash
git clone https://github.com/hashmap-kz/smallci
cd smallci
go build -o smallci .
```

## Usage

```bash
# Run with default smallci.yaml in current dir
smallci

# Specify config file
smallci path/to/config.yaml
```

## Config

```yaml
steps:
  - name: fmt
    run: gofmt -l ./...
    group: lint          # steps with the same group run sequentially

  - name: vet
    run: go vet ./...
    group: lint          # runs after fmt (same group)

  - name: unit-tests
    run: go test ./...
    group: test          # lint and test groups run in parallel

  - name: race-tests
    run: go test -race ./...
    group: test          # runs after unit-tests
```

**Groups** are the unit of parallelism. All distinct groups start simultaneously. Within a group, steps run in order. If a step fails, the rest of its group is skipped.

Steps without a `group` are each their own group (fully parallel).

## Keybindings

| Key | Action |
|-----|--------|
| `↑` / `↓` or `k` / `j` | Select step |
| `enter` | Toggle log panel expand |
| `f` | Toggle failed-only view |
| `q` / `ctrl+c` | Quit |

## Exit code

`smallci` exits `1` if any step failed, `0` otherwise — suitable for use in scripts.
