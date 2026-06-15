package main

import (
	"context"
	"os"

	"github.com/rokuosan/git-wtclean/internal/wtclean"
)

func main() {
	app := wtclean.NewApp(wtclean.ExecRunner{}, os.Stdout, os.Stderr)
	os.Exit(app.Run(context.Background(), os.Args[1:]))
}
