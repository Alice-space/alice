.DEFAULT_GOAL := build

.PHONY: build run fmt fmt-check vet test secret-check check precommit-install

build:
	go build -o bin/alice ./cmd/connector

run:
	go run ./cmd/connector

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

secret-check:
	./scripts/secret-check.sh

check: secret-check fmt-check vet test

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
