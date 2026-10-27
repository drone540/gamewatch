package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"
)

var version = "0.1.0"

func main() {
	log.SetFlags(log.Ltime)

	var (
		configPath  string
		jsonOut     bool
		once        bool
		verbose     bool
		listen      string
		webhook     string
		statusFile  string
		showVersion bool
	)
	flag.StringVar(&configPath, "config", "", "path to gamewatch.json (default: ./gamewatch.json, then next to the exe)")
	flag.BoolVar(&jsonOut, "json", false, "write events and -once output as JSON")
	flag.BoolVar(&once, "once", false, "scan once, print the result and exit (0 = game, 1 = no game)")
	flag.BoolVar(&verbose, "verbose", false, "log detection candidates and library details")
	flag.StringVar(&listen, "listen", "", "serve status at this address, e.g. 127.0.0.1:8732")
	flag.StringVar(&webhook, "webhook", "", "POST every game_started/game_stopped event to this URL")
	flag.StringVar(&statusFile, "status-file", "", "write the current status to this JSON file on every poll")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println("gamewatch", version)
		return
	}

	cfg, usedPath, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if usedPath != "" {
		log.Printf("config: %s", usedPath)
	} else {
		log.Printf("config: built-in defaults")
	}

	detector, err := NewDetector(cfg, verbose)
	if err != nil {
		log.Fatalf("init: %v", err)
	}
	agent := NewAgent(cfg, detector, jsonOut, webhook, statusFile)

	if once {
		status, err := agent.Once(time.Now())
		if err != nil {
			log.Printf("detect: %v", err)
			os.Exit(2)
		}
		if jsonOut {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetEscapeHTML(false)
			_ = encoder.Encode(status)
		} else {
			fmt.Println(formatStatus(status))
		}
		if status.Gaming {
			os.Exit(0)
		}
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if listen != "" {
		if err := startHTTPServer(ctx, listen, agent); err != nil {
			log.Fatalf("http: %v", err)
		}
	}
	agent.Run(ctx)
}
