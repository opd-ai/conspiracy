# Contributing to Conspiracy

Thank you for your interest in contributing to Conspiracy, a zero-configuration mesh networking platform! This guide will help you get started with development, testing, and submitting contributions.

## Prerequisites

- **Go 1.25+** (check with `go version`)
- **Git** for version control
- **LoRa hardware** (optional — most contributions can be developed and tested without hardware using UDP test stubs)
  - SX127x/SX126x chipset via SPI (e.g., Raspberry Pi + RAK2245 HAT)
  - USB LoRa dongle (e.g., Dragino LG02, RAK811)
- **Linux development environment** (Ubuntu 22.04+, Debian 12+, or equivalent)

## Getting Started

### 1. Clone the Repository

```bash
git clone https://github.com/opd-ai/conspiracy.git
cd conspiracy
```

### 2. Build the Project

```bash
go build -o bin/conspiracyd ./cmd/conspiracyd
```

Verify successful build:
```bash
./bin/conspiracyd
# Should output: "conspiracyd - zero-configuration mesh networking daemon"
```

### 3. Run Tests

```bash
# Run all tests with race detector
go test -race ./...

# Run tests for specific package
go test -v ./internal/lora
go test -v ./internal/crypto

# Run with coverage
go test -cover ./...
```

### 4. Verify Code Quality

```bash
# Run Go vet
go vet ./...

# Format code (required before PR submission)
go fmt ./...
```

## Development Workflow

### Project Structure

```
conspiracy/
├── cmd/
│   └── conspiracyd/        # Daemon entry point
├── internal/
│   ├── lora/               # LoRa radio driver and frame codec
│   ├── wifi/               # nl80211 mesh interface control
│   ├── batman/             # batman-adv netlink integration
│   ├── crypto/             # Security primitives (AEAD, nonces, reboot counter)
│   ├── hint/               # HintBus for layer-3 overlay plugins
│   ├── autojoin/           # JOIN_REQ/ACK state machine
│   └── config/             # TOML configuration parser
├── docs/                   # Design specifications and guides
├── examples/               # Configuration examples (EU, US, AS regions)
├── deployments/systemd/    # Systemd service units
└── scripts/                # Installation and utility scripts
```

### Code Style Guidelines

1. **Follow Effective Go**: https://golang.org/doc/effective_go
2. **Use `go fmt`**: All code must be formatted before submission
3. **Network types MUST use interfaces** (Guideline #1):
   - ✅ Use `net.Conn`, `net.PacketConn`, `net.Listener`, `net.Addr`
   - ❌ Never use `*net.TCPConn`, `*net.UDPConn`, `*net.UDPAddr`, etc.
   - This ensures testability and flexibility when working with mocks
4. **Comment exported symbols**: All public functions, types, and constants require doc comments
5. **Keep functions small**: Target ≤30 lines per function; cyclomatic complexity ≤10
6. **Use structured logging**: Prefer `log/slog` over `fmt.Printf`
7. **Error handling**: Wrap errors with context using `fmt.Errorf("...: %w", err)`

### Testing Standards

#### Unit Tests
- Place tests in `*_test.go` files alongside implementation
- Use table-driven tests for functions with multiple input variations
- Test both success and failure paths
- Example:
  ```go
  func TestNonceGenerator_Uniqueness(t *testing.T) {
      tests := []struct {
          name      string
          nodeID    uint32
          rebootCtr uint32
          wantErr   bool
      }{
          {"valid", 0x12345678, 1, false},
          {"zero_node_id", 0, 1, false},
      }
      
      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              // Test implementation
          })
      }
  }
  ```

#### Integration Tests
- Place in `test/integration/` directory
- May require real hardware or kernel modules (batman-adv, mac80211_hwsim)
- Document hardware requirements in test file header
- Use build tags for hardware-dependent tests:
  ```go
  // +build hardware
  ```

#### Running Hardware-in-the-Loop Tests
For contributors with LoRa hardware:
```bash
# Run hardware tests (requires SX127x HAT on /dev/spidev0.0)
go test -v -tags=hardware ./internal/lora

# Run batman-adv integration tests (requires kernel module)
sudo modprobe batman_adv
go test -v ./internal/batman
```

## Submitting Contributions

### Before Opening a Pull Request

1. **Run the full test suite**:
   ```bash
   go test -race ./...
   go vet ./...
   ```

2. **Update documentation** if behavior changes:
   - Update `docs/lora-mesh-design.md` for protocol changes
   - Update `README.md` for user-facing changes
   - Add/update comments for public API changes

3. **Add entry to CHANGELOG.md** (if applicable):
   ```markdown
   ## [Unreleased]
   ### Added
   - New feature description
   
   ### Fixed
   - Bug fix description
   ```

4. **Verify cross-compilation**:
   ```bash
   GOARCH=mipsle GOOS=linux go build ./cmd/conspiracyd
   GOARCH=arm64 GOOS=linux go build ./cmd/conspiracyd
   ```

### Pull Request Checklist

- [ ] Tests pass: `go test -race ./...`
- [ ] Vet passes: `go vet ./...`
- [ ] Code formatted: `go fmt ./...`
- [ ] Documentation updated (if needed)
- [ ] CHANGELOG.md updated (if user-facing change)
- [ ] Commit messages follow conventional format (see below)
- [ ] Network types use interfaces (no `*net.UDPConn`, use `net.PacketConn`)

### Commit Message Format

Use conventional commits for clear history:

```
<type>: <short description>

<optional longer description>

<optional footer>
```

**Types**:
- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation changes
- `test:` Test additions or changes
- `refactor:` Code refactoring without behavior change
- `perf:` Performance improvements
- `chore:` Tooling, dependencies, build changes

**Examples**:
```
feat: implement SX127x SPI driver with RegVersion validation

Adds proof-of-concept LoRa driver for SX127x chipsets using periph.io/x/conn/v3.
Includes register read/write primitives, chip detection (version 0x12/0x22/0x21),
and basic TX/RX operations with IRQ polling.

Closes #42
```

```
fix: prevent nonce reuse across daemon restarts

Implements persistent reboot counter with atomic write-rename to
/var/lib/conspiracyd/reboot_counter. Daemon refuses to start LoRa subsystem
if counter persistence fails (disk full, read-only mount).

Addresses AUDIT.md finding "Security Implementation Complexity".
```

## Development Environment Setup (Optional)

### VS Code

Recommended extensions:
- **Go** (golang.go) — official Go extension
- **Go Test Explorer** — visual test runner

### Vim/Neovim

Recommended plugins:
- **vim-go** or **nvim-lspconfig** with gopls

## Community and Support

- **Mailing List**: TBD
- **IRC**: TBD
- **Issue Tracker**: https://github.com/opd-ai/conspiracy/issues

## Code of Conduct

Be respectful, inclusive, and constructive. We follow the [Contributor Covenant](https://www.contributor-covenant.org/).

## License

By contributing, you agree that your contributions will be licensed under the GNU Affero General Public License v3.0 (AGPL-3.0). Network server operators must make modified source code available to users.

---

**Questions?** Open an issue or reach out on the mailing list. Happy coding! 🚀
