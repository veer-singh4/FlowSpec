package main

import (
	"fmt"
	"os"

	"github.com/veer-singh4/FlowSpec/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
