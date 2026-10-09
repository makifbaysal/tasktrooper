SHELL := /bin/bash
.DEFAULT_GOAL := help

UI := desktop/ui

.PHONY: help setup dev desktop package build test lint clean

help:
	@echo "make setup     install Go modules and npm packages"
	@echo "make dev       backend (embedded Postgres) + UI dev server in the terminal"
	@echo "make desktop   run the Electron app in dev mode (builds the backend binary + UI)"
	@echo "make package   build the installer for this OS into desktop/release"
	@echo "make build     compile everything without running"
	@echo "make test      go test + typecheck/lint/test for the UI and the shell"
	@echo "make clean     remove build output (keeps server/data)"

setup:
	cd server && go mod download
	npm --prefix $(UI) ci
	npm --prefix desktop ci

dev:
	./scripts/dev.sh

desktop:
	npm --prefix desktop run dev

package:
ifeq ($(OS),Windows_NT)
	npm --prefix desktop run package:win
else ifeq ($(shell uname -s),Linux)
	npm --prefix desktop run package:linux
else
	npm --prefix desktop run package
endif

build:
	cd server && go build ./...
	npm --prefix $(UI) run build
	npm --prefix desktop run build:server
	npm --prefix desktop run build

test:
	cd server && go vet ./... && go test ./...
	cd desktop/runner && go vet ./... && go test ./...
	npm --prefix $(UI) run typecheck && npm --prefix $(UI) run build
	cd desktop && npm run typecheck && npm run lint && npm test

lint:
	cd server && go vet ./...
	cd desktop/runner && go vet ./...
	npm --prefix $(UI) run typecheck
	cd desktop && npm run typecheck && npm run lint

clean:
	rm -rf $(UI)/dist desktop/dist desktop/bin desktop/release
