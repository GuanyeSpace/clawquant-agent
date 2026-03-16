package main

import (
	"log"
	"os"

	"github.com/GuanyeSpace/clawquant-agent/internal/app"
	"github.com/GuanyeSpace/clawquant-agent/internal/buildinfo"
)

func main() {
	runner := app.Runner{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Info:   buildinfo.Current(),
	}

	if err := runner.Run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
