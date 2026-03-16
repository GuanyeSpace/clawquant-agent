APP_NAME := clawquant-agent
MODULE := github.com/GuanyeSpace/clawquant-agent
CMD_PATH := ./cmd/$(APP_NAME)
DIST_DIR := dist
VERSION ?= dev
COMMIT ?= none
BUILD_TIME ?= unknown
GO ?= go
EXE_SUFFIX :=

ifeq ($(OS),Windows_NT)
EXE_SUFFIX := .exe
MKDIR := powershell -NoProfile -Command "New-Item -ItemType Directory -Force '$(DIST_DIR)' | Out-Null"
RMDIR := powershell -NoProfile -Command "if (Test-Path '$(DIST_DIR)') { Remove-Item -Recurse -Force '$(DIST_DIR)' }"
else
MKDIR := mkdir -p $(DIST_DIR)
RMDIR := rm -rf $(DIST_DIR)
endif

LDFLAGS := -s -w \
	-X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
	-X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(MODULE)/internal/buildinfo.BuildTime=$(BUILD_TIME)

.PHONY: help fmt test tidy build run cross-build clean

help:
	@echo "Targets:"
	@echo "  make fmt         - format Go source"
	@echo "  make test        - run unit tests"
	@echo "  make tidy        - sync go.mod/go.sum"
	@echo "  make build       - build current platform binary into dist/"
	@echo "  make run         - run the CLI locally"
	@echo "  make cross-build - build windows/linux/darwin binaries"
	@echo "  make clean       - remove dist/"

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy

build:
	$(MKDIR)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)$(EXE_SUFFIX) $(CMD_PATH)

run:
	$(GO) run $(CMD_PATH)

cross-build:
	powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-cross.ps1 -ProjectName $(APP_NAME) -ModulePath $(MODULE) -Version "$(VERSION)" -Commit "$(COMMIT)" -BuildTime "$(BUILD_TIME)"

clean:
	$(RMDIR)
