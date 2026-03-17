package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/GuanyeSpace/clawquant-agent/internal/buildinfo"
	"github.com/GuanyeSpace/clawquant-agent/internal/command"
	"github.com/GuanyeSpace/clawquant-agent/internal/connection"
	"github.com/GuanyeSpace/clawquant-agent/internal/process"
	"github.com/GuanyeSpace/clawquant-agent/internal/storage"
)

func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer, info buildinfo.Info) error {
	parsed, err := ParseArgs(args, stderr)
	if err != nil {
		return handleParseError(err)
	}

	if parsed.ShowVersion {
		_, err := fmt.Fprintln(stdout, info.String())
		return err
	}

	logger := log.New(stderr, "", log.LstdFlags)

	store, dbPath, err := storage.OpenSQLite(ctx, parsed.Config.DataDir)
	if err != nil {
		return fmt.Errorf("initialize sqlite: %w", err)
	}
	defer store.Close()

	logger.Printf("SQLite initialized at %s", dbPath)

	dispatcher := command.NewDispatcher(logger)
	sdkDir, err := resolveSDKDir()
	if err != nil {
		return err
	}

	processManager, err := process.NewManager(parsed.Config.DataDir, sdkDir, store, logger)
	if err != nil {
		return fmt.Errorf("initialize process manager: %w", err)
	}

	manager := connection.NewManager(parsed.Config.Token, parsed.Config.Secret, parsed.Config.Server, dispatcher, processManager, store, logger)
	processManager.SetConnectionManager(manager)
	dispatcher.SetController(processManager)
	dispatcher.SetSender(manager)

	if err := manager.Connect(ctx); err != nil {
		return fmt.Errorf("connect to platform: %w", err)
	}

	logger.Print("Connected to platform")

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := processManager.StopAll(shutdownCtx); err != nil {
		logger.Printf("stop child processes: %v", err)
	}

	if err := manager.Close(); err != nil && !errors.Is(err, context.Canceled) {
		logger.Printf("close websocket manager: %v", err)
	}

	logger.Print("Agent shutdown complete")
	return nil
}

func handleParseError(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}

	if errors.Is(err, ErrMissingRequiredFlag) {
		return err
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return nil
	}

	return err
}

func resolveSDKDir() (string, error) {
	var candidates []string

	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "sdk"))
	}

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "sdk"),
			filepath.Join(exeDir, "..", "sdk"),
		)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		absPath, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, ok := seen[absPath]; ok {
			continue
		}
		seen[absPath] = struct{}{}

		if info, err := os.Stat(absPath); err == nil && info.IsDir() {
			return absPath, nil
		}
	}

	return "", fmt.Errorf("sdk directory not found; expected ./sdk near the working directory or executable")
}
