# make dist builds dist/waifuwebshelf-linux-amd64.tar.gz. scripts/deploy.sh runs it
# with the server's ARCH. Use GO=~/.local/go/bin/go when go isn't on PATH.
GO ?= go
ARCH ?= amd64
TARBALL := dist/waifuwebshelf-linux-$(ARCH).tar.gz

.PHONY: dist test

# The tarball holds app/ (binary, templates, static, service, env example) and bin/install.sh.
dist:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) $(GO) build -trimpath -o dist/waifuwebshelf-$(ARCH) ./cmd/waifuwebshelf
	tar -czf $(TARBALL) --owner=0 --group=0 \
		--transform='s,^dist/waifuwebshelf-$(ARCH)$$,app/waifuwebshelf,;s,^examples/,app/,;s,^scripts/,bin/,;s,^templates,app/templates,;s,^static,app/static,' \
		dist/waifuwebshelf-$(ARCH) templates static examples/waifuwebshelf.service examples/env.example scripts/install.sh
	@echo "Built $(TARBALL)"

test:
	$(GO) test ./...
