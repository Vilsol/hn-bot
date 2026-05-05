package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "log what would be sent without contacting Telegram or mutating state")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [-dry-run] <bot-token> <chat-id>\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(2)
	}
	token, chatID := flag.Arg(0), flag.Arg(1)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	state, err := LoadState("parsed.json")
	if err != nil {
		log.Fatalf("load state: %v", err)
	}
	log.Printf("Loaded %d parsed posts!", len(state.Posts))

	tg, err := NewTelegram(ctx, token, chatID)
	if err != nil {
		log.Fatalf("telegram: %v", err)
	}
	log.Printf("Connected to '%s' chat!", chatID)

	app := &App{
		State:   state,
		HN:      NewHN(),
		TG:      tg,
		Backlog: NewBacklog(),
		DryRun:  *dryRun,
	}
	if *dryRun {
		log.Printf("DRY RUN: state and Telegram will not be touched")
	}
	app.Run(ctx)
}
