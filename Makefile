SHELL := /bin/bash
.DEFAULT_GOAL := help

UI := desktop/ui

.PHONY: help setup dev desktop package build test lint clean

help:
	@echo "make setup     install Go modules and npm packages"
	@echo "make dev       backend (embedded Postgres) + UI dev server in the terminal"
	@echo "make desktop   run the Electron app in dev mode (builds the backend binary + UI)"
	@echo "make package   build the distributable macOS app into desktop/release"
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
	npm --prefix desktop run package

build:
	cd server && go build ./...
	npm --prefix $(UI) run build
	npm --prefix desktop run build:server
	npm --prefix desktop run build

test:
	cd server && go vet ./... && go test ./...
	cd $(UI) && npx tsc --noEmit && npm run build
	cd desktop && npm run typecheck && npm run lint && npm test

lint:
	cd server && go vet ./...
	cd $(UI) && npx tsc --noEmit
	cd desktop && npm run typecheck && npm run lint

clean:
	rm -rf $(UI)/dist desktop/dist desktop/bin desktop/release
