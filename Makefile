SHELL := /bin/bash

# Everything the build needs lives under .cache so a checkout stays
# self-contained and does not touch ~/go.
export GOPATH := $(CURDIR)/.cache/go
export GOMODCACHE := $(CURDIR)/.cache/mod
export GOCACHE := $(CURDIR)/.cache/build
export GOTOOLCHAIN := go1.27.4
# mixed/gvisor TUN stacks require this feature in mihomo.
export GOFLAGS ?= -tags=with_gvisor

# Keep the frontend build cache inside the checkout, like the Go caches.
NPM_CONFIG_CACHE ?= $(CURDIR)/.cache/npm
export NPM_CONFIG_CACHE

BIN := bin/Gopurr
NSH := $(CURDIR)/.cache/nsh/nsh/bin/nsh
NSH_TSC ?= $(shell command -v tsc 2>/dev/null)
NATIVE_TARGET ?= catalyst
NATIVE_ANDROID_ABI ?= arm64-v8a
NATIVE_ANDROID_SERIAL ?= emulator-5580

.PHONY: run build bundle brand-master icons preview-icons dev web web-dev tidy clean autostart test test-native update-geodata
.PHONY: native-windows native-tools native-ui native-desktop-ui native-check native-test native-bundle native-run native-smoke native-desktop-smoke native-android native-android-release native-android-test

# Native preview targets stay isolated. The macOS bundle/release entry point
# below ships NSH using the established desktop identity and update trust.
native-windows:
	pwsh -NoProfile -File scripts/build-native-windows.ps1

native-tools:
	bash scripts/bootstrap-nsh.sh

native-ui: native-tools
	python3 native/ui/sync-assets.py
	$(NSH) build native/ui/App.tsx --release --out bin/native/ui

native-desktop-ui: native-tools
	python3 native/ui/sync-assets.py
	$(NSH) build native/ui/Desktop.tsx --release --out bin/native/desktop-ui

native-check: native-tools
	python3 native/ui/sync-assets.py
	$(NSH) check native/ui $(if $(NSH_TSC),--tsc "$(NSH_TSC)",)

native-test: native-tools
	python3 native/ui/sync-assets.py
	go test ./sdk/engine ./internal/workflow ./internal/profile ./internal/nativehost ./internal/app ./cmd/gopurr-bridge
	$(NSH) test native/ui

native-bundle: native-ui
	bash scripts/build-native-apple.sh $(NATIVE_TARGET)

native-run: native-bundle
	open "bin/native/Gopurr Native.app"

native-android: native-ui
	bash scripts/build-native-android.sh $(NATIVE_ANDROID_ABI)

# See native/RELEASING.md for the required Android signing environment.
native-android-release:
	bash scripts/build-native-android.sh $(NATIVE_ANDROID_ABI) release "$(NATIVE_ANDROID_VERSION)"

native-android-test: native-android
	GOPURR_TEST_ANDROID_ABI=$(NATIVE_ANDROID_ABI) bash scripts/test-native-android.sh $(NATIVE_ANDROID_SERIAL)

# ctypes checks the native ABI, then sends real HTTP through the isolated
# engine's SOCKS listener to a temporary local server. It changes no OS settings.
native-smoke:
	mkdir -p .cache/native-smoke
	CGO_ENABLED=1 go build -buildmode=c-shared -o .cache/native-smoke/libgopurr.dylib ./cmd/gopurr-bridge
	python3 scripts/smoke-native-bridge.py --library "$(CURDIR)/.cache/native-smoke/libgopurr.dylib"

native-desktop-smoke: native-bundle
	python3 scripts/smoke-native-desktop.py

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
	go run scripts/generate-android-icons.go

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
