.DEFAULT_GOAL := build

.PHONY: build run fmt fmt-check vet test race secret-check script-check check precommit-install docs-build docs-serve

build:
	go build -o bin/alice ./cmd/connector

run:
	go run ./cmd/connector --feishu-websocket

fmt:
	gofmt -w $(shell find . -name '*.go' -type f)

fmt-check:
	@unformatted=$$(gofmt -l $$(find . -name '*.go' -type f)); \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./internal/connector

secret-check:
	./scripts/secret-check.sh

script-check:
	@for f in $$(find scripts -type f -name '*.sh' | sort); do \
		bash -n "$$f"; \
	done

check: secret-check script-check fmt-check vet test race

precommit-install:
	@mkdir -p .githooks
	@cp scripts/pre-commit.sh .githooks/pre-commit
	@cp scripts/commit-msg.sh .githooks/commit-msg
	@chmod +x .githooks/pre-commit
	@chmod +x .githooks/commit-msg
	@git config core.hooksPath .githooks
	@echo "Installed git hooks:"
	@echo "  - .githooks/pre-commit"
	@echo "  - .githooks/commit-msg"

docs-build:
	@command -v mdbook >/dev/null 2>&1 || { echo "mdbook not found. Install: brew install mdbook"; exit 1; }
	rm -rf book/out
	mdbook build book/en --dest-dir ../../out/en
	mdbook build book/zh --dest-dir ../../out/zh
	cp book/index.html out/
	cp book/lang-switcher.js out/
	@echo "Docs built → book/out/"

docs-serve:
	@command -v mdbook >/dev/null 2>&1 || { echo "mdbook not found. Install: brew install mdbook"; exit 1; }
	mdbook serve book/en
