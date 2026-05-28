// Command guard is the CLI entrypoint for Sentinel, the on-device secure
// execution layer. It turns natural-language ops tasks into concrete commands
// using a local model, screens them through the Policy Guard, and runs them
// with a human in the loop.
package main

import (
	"os"

	"github.com/xiaokhkh/sentinel/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
