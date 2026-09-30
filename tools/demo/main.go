package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: %s run|seed|migrate|commands [flags]", os.Args[0])
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	switch args[0] {
	case "run":
		return runDemo(ctx, args[1:])
	case "seed":
		return seed(ctx, args[1:])
	case "migrate":
		return migrate(ctx, args[1:])
	case "commands":
		return commands(ctx, args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}
