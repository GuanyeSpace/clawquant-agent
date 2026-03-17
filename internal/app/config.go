package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

type Config struct {
	Token   string
	Secret  string
	Server  string
	DataDir string
}

type ParseResult struct {
	Config      Config
	ShowVersion bool
}

func ParseArgs(args []string, stderr io.Writer) (ParseResult, error) {
	var result ParseResult

	fs := flag.NewFlagSet("clawquant-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&result.Config.Token, "token", "", "Agent token")
	fs.StringVar(&result.Config.Secret, "secret", "", "Agent secret")
	fs.StringVar(&result.Config.Server, "server", "", "Platform WebSocket server, for example wss://api.clawquant.com")
	fs.StringVar(&result.Config.DataDir, "data-dir", "./data", "Directory used for local agent state")
	fs.BoolVar(&result.ShowVersion, "version", false, "Print build information")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: clawquant-agent --token <token> --secret <secret> --server <ws://host>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Flags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return ParseResult{}, err
	}

	if result.ShowVersion {
		return result, nil
	}

	var missing []string
	if strings.TrimSpace(result.Config.Token) == "" {
		missing = append(missing, "--token")
	}

	if strings.TrimSpace(result.Config.Secret) == "" {
		missing = append(missing, "--secret")
	}

	if strings.TrimSpace(result.Config.Server) == "" {
		missing = append(missing, "--server")
	}

	if len(missing) > 0 {
		return ParseResult{}, fmt.Errorf("%w: %s", ErrMissingRequiredFlag, strings.Join(missing, ", "))
	}

	return result, nil
}

var ErrMissingRequiredFlag = errors.New("missing required flag")
