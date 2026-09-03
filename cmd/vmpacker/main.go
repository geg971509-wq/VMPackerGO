package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/geg971509-wq/VMPackerGO/internal/app"
)

func main() {
	err := app.RunWithConfig(context.Background(), os.Args[1:], os.Stdout, os.Stderr, app.Config{
		Version: version,
		Commit:  commit,
	})
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintf(os.Stderr, "vmpacker: %v\n", err)
	}
	os.Exit(app.ExitCode(err))
}
