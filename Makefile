APP_NAME := clawquant-agent
MODULE := github.com/GuanyeSpace/clawquant-agent
CMD_PATH := ./cmd/agent
BIN_DIR := bin
DIST_DIR := dist
VERSION ?= dev
COMMIT ?= none
BUILD_TIME ?= unknown
GO ?= go
EXE_SUFFIX :=

ifeq ($(OS),Windows_NT)
EXE_SUFFIX := .exe
MKDIR_BIN := powershell -NoProfile -Command "New-Item -ItemType Directory -Force '$(BIN_DIR)' | Out-Null"
MKDIR_DIST := powershell -NoProfile -Command "New-Item -ItemType Directory -Force '$(DIST_DIR)' | Out-Null"
RMDIR_BIN := powershell -NoProfile -Command "if (Test-Path '$(BIN_DIR)') { Remove-Item -Recurse -Force '$(BIN_DIR)' }"
RMDIR_DIST := powershell -NoProfile -Command "if (Test-Path '$(DIST_DIR)') { Remove-Item -Recurse -Force '$(DIST_DIR)' }"
else
MKDIR_BIN := mkdir -p $(BIN_DIR)
MKDIR_DIST := mkdir -p $(DIST_DIR)
RMDIR_BIN := rm -rf $(BIN_DIR)
RMDIR_DIST := rm -rf $(DIST_DIR)
endif

LDFLAGS := -s -w \
	-X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
	-X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(MODULE)/internal/buildinfo.BuildTime=$(BUILD_TIME)

.PHONY: help fmt test tidy build build-linux run cross-build clean

help:
	@echo "Targets:"
	@echo "  make fmt         - format Go source"
	@echo "  make test        - run unit tests"
	@echo "  make tidy        - sync go.mod/go.sum"
	@echo "  make build       - build current platform binary into bin/"
	@echo "  make build-linux - cross-compile linux/amd64 into bin/"
	@echo "  make run         - run the agent locally with test credentials"
	@echo "  make cross-build - build windows/linux/darwin binaries"
	@echo "  make clean       - remove bin/ and dist/"

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy

build:
	$(MKDIR_BIN)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)$(EXE_SUFFIX) $(CMD_PATH)

build-linux:
	$(MKDIR_BIN)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME)-linux-amd64 $(CMD_PATH)

run:
	$(GO) run $(CMD_PATH) --token test --secret test --server ws://localhost:8080

cross-build:
	$(MKDIR_DIST)
	powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-cross.ps1 -ProjectName $(APP_NAME) -ModulePath $(MODULE) -MainPackage $(CMD_PATH) -Version "$(VERSION)" -Commit "$(COMMIT)" -BuildTime "$(BUILD_TIME)"

clean:
	$(RMDIR_BIN)
	$(RMDIR_DIST)
