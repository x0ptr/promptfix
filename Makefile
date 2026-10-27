BINARY  := promptfix
PKG     := ./cmd/promptfix
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/x0ptr/promptfix/internal/cli.Version=$(VERSION)
GOFLAGS := -trimpath
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

export CGO_ENABLED := 0

.PHONY: build test vet dist clean

build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

dist:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		out=dist/$(BINARY)-$(VERSION)-$$os-$$arch$$ext; \
		echo "build $$out"; \
		GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $$out $(PKG) || exit 1; \
	done

clean:
	rm -rf bin dist
