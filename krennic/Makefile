# Krennic — build + cross-compile matrix

BINARY   := krennic
PKG      := ./cmd/krennic
DIST     := dist
VERSION  := $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)

# Pure-Go (CGO disabled) keeps cross-compilation trivial — modernc SQLite, no cgo.
export CGO_ENABLED := 0

.PHONY: all build test vet tidy clean dist \
        dist-linux-amd64 dist-linux-arm64 dist-darwin-amd64 dist-darwin-arm64 dist-windows-amd64

all: test build

build:
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf $(DIST)

# Cross-compile all target platforms for the 10-machine fleet.
dist: dist-linux-amd64 dist-linux-arm64 dist-darwin-amd64 dist-darwin-arm64 dist-windows-amd64

dist-linux-amd64:
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-amd64   $(PKG)
dist-linux-arm64:
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-arm64   $(PKG)
dist-darwin-amd64:
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-amd64  $(PKG)
dist-darwin-arm64:
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-darwin-arm64  $(PKG)
dist-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-windows-amd64.exe $(PKG)
