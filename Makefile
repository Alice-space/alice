.PHONY: format lint type test check precommit-install

VENV ?= .venv
PYTHON ?= $(VENV)/bin/python
BLACK ?= $(VENV)/bin/black
ISORT ?= $(VENV)/bin/isort
MYPY ?= $(VENV)/bin/mypy
PYTEST ?= $(VENV)/bin/pytest
PRECOMMIT ?= $(VENV)/bin/pre-commit

format:
	$(ISORT) app tests
	$(BLACK) app tests

lint:
	$(ISORT) --check-only app tests
	$(BLACK) --check app tests

type:
	$(MYPY) app tests

test:
	$(PYTEST) -q

check: lint type test

precommit-install:
	$(PRECOMMIT) install
