package app

import (
	"flag"
	"fmt"
	"io"

	"github.com/GuanyeSpace/clawquant-agent/internal/buildinfo"
)

type Runner struct {
	Stdout io.Writer
	Stderr io.Writer
	Info   buildinfo.Info
}

func (r Runner) Run(args []string) error {
	fs := flag.NewFlagSet("clawquant-agent", flag.ContinueOnError)
	fs.SetOutput(r.Stderr)

	showVersion := fs.Bool("version", false, "print build information")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		_, err := fmt.Fprintln(r.Stdout, r.Info.String())
		return err
	}

	_, err := fmt.Fprintln(r.Stdout, "clawquant-agent initialized. Add your strategy/runtime wiring under internal/.")
	return err
}
