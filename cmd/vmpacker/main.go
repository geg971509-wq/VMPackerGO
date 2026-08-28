package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/vmpacker/internal/app"
)

//go:embed vm_interp.bin
var interpBlob []byte

func main() {
	err := app.RunWithConfig(context.Background(), os.Args[1:], os.Stdout, os.Stderr, app.Config{
		InterpBlob: interpBlob,
		Version:    version,
		Commit:     commit,
	})
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintf(os.Stderr, "vmpacker: %v\n", err)
	}
	os.Exit(app.ExitCode(err))
}
