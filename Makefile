.PHONY: build clean prepare-embed test release install build-all setup check-deps

# Check for required dependencies
check-deps:
	@echo "🔍 Checking dependencies..."
	@command -v go >/dev/null 2>&1 || { echo "❌ Go is not installed. Install from https://go.dev/dl/"; exit 1; }
	@command -v node >/dev/null 2>&1 || { echo "❌ Node.js is not installed. Install from https://nodejs.org/"; exit 1; }
	@command -v npm >/dev/null 2>&1 || { echo "❌ npm is not installed. Install Node.js from https://nodejs.org/"; exit 1; }
	@echo "  ✓ Go $(shell go version | cut -d' ' -f3)"
	@echo "  ✓ Node.js $(shell node --version)"
	@echo "  ✓ npm $(shell npm --version)"

# Setup project - install all dependencies
setup: check-deps
	@echo "📦 Setting up project..."
	@echo "→ Installing Go dependencies..."
	@cd unitune-cli && go mod download
	@echo "→ Installing Node.js dependencies..."
	@cd unitune-infra && npm install
	@echo "✅ Setup complete! Run 'make build' to build the CLI."

# Prepare embedded infra (copy unitune-infra to embedded location)
prepare-embed:
	@echo "📦 Preparing embedded infrastructure..."
	@rm -rf unitune-cli/pkg/infra/embedded
	@mkdir -p unitune-cli/pkg/infra/embedded
	@cp -r unitune-infra/bin unitune-cli/pkg/infra/embedded/
	@cp -r unitune-infra/lib unitune-cli/pkg/infra/embedded/
	@cp unitune-infra/package.json unitune-cli/pkg/infra/embedded/
	@cp unitune-infra/package-lock.json unitune-cli/pkg/infra/embedded/
	@cp unitune-infra/tsconfig.json unitune-cli/pkg/infra/embedded/
	@cp unitune-infra/cdk.json unitune-cli/pkg/infra/embedded/
	@echo "✅ Infrastructure embedded"

# Build the CLI
build: prepare-embed
	@echo "🔨 Building unitune CLI..."
	@cd unitune-cli && go build -o ../bin/unitune ./cmd/unitune
	@echo "✅ Built: bin/unitune"

# Build for all platforms
build-all: prepare-embed
	@echo "🔨 Building for all platforms..."
	@mkdir -p bin
	@cd unitune-cli && GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o ../bin/unitune-darwin-arm64 ./cmd/unitune
	@cd unitune-cli && GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o ../bin/unitune-darwin-amd64 ./cmd/unitune
	@cd unitune-cli && GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ../bin/unitune-linux-amd64 ./cmd/unitune
	@cd unitune-cli && GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o ../bin/unitune-linux-arm64 ./cmd/unitune
	@echo "✅ Built all platforms in bin/"

# Clean build artifacts
clean:
	@rm -rf bin/
	@rm -rf unitune-cli/pkg/infra/embedded
	@echo "🧹 Cleaned"

# Run tests
test:
	@cd unitune-cli && go test ./...

# Install locally
install: build
	@sudo cp bin/unitune /usr/local/bin/unitune
	@echo "✅ Installed to /usr/local/bin/unitune"

# Create release tarballs
release: build-all
	@echo "📦 Creating release tarballs..."
	@mkdir -p dist
	@cd bin && tar -czvf ../dist/unitune-darwin-arm64.tar.gz unitune-darwin-arm64
	@cd bin && tar -czvf ../dist/unitune-darwin-amd64.tar.gz unitune-darwin-amd64
	@cd bin && tar -czvf ../dist/unitune-linux-amd64.tar.gz unitune-linux-amd64
	@cd bin && tar -czvf ../dist/unitune-linux-arm64.tar.gz unitune-linux-arm64
	@echo "✅ Release tarballs created in dist/"

# Development: run without building
dev: prepare-embed
	@cd unitune-cli && go run ./cmd/unitune $(ARGS)

# Show help
help:
	@echo "Available targets:"
	@echo "  setup        - Install all dependencies (run this first)"
	@echo "  build        - Build the CLI for current platform"
	@echo "  build-all    - Build for all platforms (darwin/linux, amd64/arm64)"
	@echo "  clean        - Remove build artifacts"
	@echo "  install      - Install to /usr/local/bin"
	@echo "  test         - Run tests"
	@echo "  release      - Create release tarballs"
	@echo "  dev          - Run without building (use ARGS=... for arguments)"
	@echo "  check-deps   - Verify required tools are installed"
	@echo "  help         - Show this help"

