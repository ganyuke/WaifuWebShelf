# make dist builds dist/waifuwebshelf-linux-amd64.tar.gz for scripts/deploy.sh.
# Use ARCH=arm64 for a Raspberry Pi, and GO=~/.local/go/bin/go when go isn't on PATH.
GO ?= go
ARCH ?= amd64
TARBALL := dist/waifuwebshelf-linux-$(ARCH).tar.gz

.PHONY: dist test clean

dist:
	rm -rf dist/stage && mkdir -p dist/stage/app dist/stage/bin
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) $(GO) build -trimpath -o dist/stage/app/waifuwebshelf ./cmd/waifuwebshelf
	cp -r templates static examples/waifuwebshelf.service examples/env.example dist/stage/app/
	cp scripts/install.sh dist/stage/bin/
	tar -czf $(TARBALL) --owner=0 --group=0 -C dist/stage app bin
	rm -rf dist/stage
	@echo "Built $(TARBALL)"

test:
	$(GO) test ./...

clean:
	rm -rf dist
