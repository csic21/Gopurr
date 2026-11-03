SHELL := /bin/bash

# Everything the build needs lives under .cache so a checkout stays
# self-contained and does not touch ~/go.
export GOPATH := $(CURDIR)/.cache/go
export GOMODCACHE := $(CURDIR)/.cache/mod
export GOCACHE := $(CURDIR)/.cache/build
export GOTOOLCHAIN := go1.27.3
# mixed/gvisor TUN stacks require this feature in mihomo.
export GOFLAGS ?= -tags=with_gvisor

# Keep the frontend build cache inside the checkout, like the Go caches.
NPM_CONFIG_CACHE ?= $(CURDIR)/.cache/npm
export NPM_CONFIG_CACHE

BIN := bin/Gopurr

.PHONY: run build bundle brand-master icons preview-icons dev web web-dev tidy clean autostart test test-native update-geodata

build:
	go build -o $(BIN) .

# A macOS app bundle with the application icon in Finder as well as the Dock.
bundle:
	go run ./scripts/release -platform darwin/arm64 -skip-dmg

# Refresh the first-launch baseline; installed apps update independently.
update-geodata:
	go run ./scripts/update-geodata

# Runtime sizes and the black-and-transparent menu-bar template.
icons:
	go run scripts/generate-icons.go
	go run scripts/generate-windows-tray.go

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

# Rebuild the embedded remote console. The generated files are committed so
# ordinary Go builds remain Node-free; release builds run this before Go.
web:
	cp internal/brand/logo.svg web/public/assets/logo.svg
	cd web && npm ci --no-audit --no-fund && npm run build

web-dev:
	cd web && npm run dev

test:
	go test ./...

# Opt-in macOS window lifecycle checks using an isolated local controller.
test-native:
	go test -tags gopurr_native . -count=1 -timeout=60s

tidy:
	go mod tidy

clean:
	rm -rf bin
