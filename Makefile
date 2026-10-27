SHELL := /bin/bash

# Everything the build needs lives under .cache so a checkout stays
# self-contained and does not touch ~/go.
export GOPATH := $(CURDIR)/.cache/go
export GOMODCACHE := $(CURDIR)/.cache/mod
export GOCACHE := $(CURDIR)/.cache/build
export GOTOOLCHAIN := go1.27.3

BIN := bin/Gopurr

.PHONY: run build bundle brand-master icons preview-icons dev tidy clean autostart test

build:
	go build -o $(BIN) .

# A macOS app bundle with the application icon in Finder as well as the Dock.
bundle:
	go run ./scripts/release -platform darwin/arm64 -skip-dmg

# Runtime sizes and the black-and-transparent menu-bar template.
icons:
	go run scripts/generate-icons.go

# Re-render the flat vector design, then derive the runtime sizes.
brand-master:
	go run scripts/generate-icons.go -render-master

preview-icons:
	go run scripts/preview-icons.go

run: build
	$(BIN)

# Compatibility alias: normal startup already starts the kernel by default.
autostart: build
	GOPURR_AUTOSTART=1 $(BIN)

dev:
	go tool mygo dev

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf bin
