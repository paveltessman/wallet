// Command wallet runs the wallet service.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

type command struct {
	name        string
	description string
	callback    func(context.Context, []string) error
}

func commands() []command {
	commands := []command{
		{"serve", "Run the HTTP server", serve},
		{"migrate", "Apply or roll back database migrations (up|down|status)", runMigrate},
		{"healthcheck", "Check that the local server is ready", runHealthcheck},
	}
	return commands
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wallet:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("no command given")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	for _, c := range commands() {
		if c.name == args[0] {
			return c.callback(ctx, args[1:])
		}
	}
	usage()
	return fmt.Errorf("unknown command %q", args[0])
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wallet <command> [args]")
	fmt.Fprintln(os.Stderr, "\ncommands:")
	for _, c := range commands() {
		fmt.Fprintf(os.Stderr, "  %-12s %s\n", c.name, c.description)
	}
}
