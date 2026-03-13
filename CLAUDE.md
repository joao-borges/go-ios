# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

go-ios is a Go library and CLI tool for automating iOS devices on Linux, Windows, and macOS. It communicates with devices over USB (via usbmuxd) and network connections, implementing Apple's proprietary protocols (usbmux, lockdown, DTX, XPC, plist). JSON output by default. Used in production by companies like Headspin.io and Sauce Labs.

## Build & Development Commands

```bash
# Build main binary (outputs ./ios)
go build -o ios ./main.go

# Build both ios and go-ncm binaries
make build

# Run linter (golangci-lint with forbidigo - no bare print/println)
make lint

# Run fast unit tests
go test -v -tags=fast ./...

# Run a single test
go test -v -tags=fast -run TestName ./ios/packagename/

# Run all tests including integration tests (requires real device)
go test -v ./...

# Check code formatting
gofmt -l .

# Setup git hooks
make setup
```

## Key Architecture

**Go workspace** with three modules: root (main CLI + ios library), `./ncm` (CDC-NCM network driver, requires CGO), `./restapi` (experimental REST API).

### Device Connection Flow

1. Connect to usbmuxd (Unix socket on macOS/Linux, TCP port 27015 on Windows; override with `USBMUXD_SOCKET_ADDRESS`)
2. Enumerate/listen for devices
3. Pair device (handles trust dialog)
4. Connect to device services via lockdown on specific ports
5. Enable SSL, then communicate via protocol (plist, DTX, XPC)
6. iOS 17+ uses QUIC-based tunnels (`ios/tunnel/`)

### Core Packages (all under `ios/`)

- **Root `ios/`**: Device connection (`deviceconnection.go`), usbmux (`usbmuxconnection.go`), pairing (`pair.go`), discovery, lockdown protocol
- **`tunnel/`**: iOS 17+ device tunnels via QUIC with platform-specific TUN implementations
- **`dtx_codec/`**: DTX (Device-to-Host eXchange) protocol codec
- **`xpc/`**: XPC message protocol
- **`nskeyedarchiver/`**: NSKeyedArchive serialization
- **`testmanagerd/`**: XCUITest runner (largest package, ~27K lines) with version-specific runners (`xcuitestrunner_11.go`, `xcuitestrunner_12.go`)
- **`instruments/`**: Process control, device info, screenshots
- **`afc/`**: Apple File Connection (file operations on device)
- **`installationproxy/`**: App install/uninstall
- **`debugproxy/`**: Debug proxy for reverse-engineering Apple protocols

### CLI (`main.go`)

Single monolithic file (~2900 lines) using **docopt** for argument parsing. 60+ commands. The `Main()` function is exported for testing. Device selection via `--udid`, debug logging with `-v`, trace with `-t`, disable JSON with `--nojson`.

## Testing Conventions

- **Fast tests**: Use build tag `//go:build !fast` on integration tests so `go test -tags=fast ./...` skips them
- **Integration tests**: Named `*_integration_test.go`, require a real connected iOS device
- **Assertions**: Uses `stretchr/testify`

## Code Style

- No bare `print`/`println` — use `fmt.Print*` or `log.*` (enforced by linter)
- Code must pass `gofmt` formatting check
- Logging via `github.com/sirupsen/logrus`
