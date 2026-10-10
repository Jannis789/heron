.PHONY: run build test lint tidy check nix shot

run:
	go run .

build:
	go build -o bin/heron .

test:
	go test ./...

lint:
	go vet ./...

tidy:
	go mod tidy

# Schnelles Gate: Vet + Tests + Build in einem Lauf (FR-14/FR-15).
check: lint test build
	@echo "check ok"

# Flaken-Bau (benötigt committeten Baum, weil Nix den Git-Tree copiert).
nix:
	nix build .#default

# Visuelle TUI-Pruefung: pty-Runner, schreibt den Screen nach stdout.
shot:
	python3 scripts/tui-shot --demo --seed examples/reply.md --keys "hallo\r"
