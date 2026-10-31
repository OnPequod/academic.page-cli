// Command academic-page manages an Academic.page site through its public API.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/OnPequod/academic.page-cli/internal/cli"
)

// Set by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, cli.BuildInfo{Version: version, Commit: commit, Date: date}, os.Args[1:])
	stop()
	os.Exit(code)
}
