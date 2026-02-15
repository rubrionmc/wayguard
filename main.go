package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var configPath string

	flag.StringVar(&configPath, "c", "", "Path to config file")
	flag.StringVar(&configPath, "config", "", "Path to config file")

	flag.Parse()

	if configPath == "" {
		configPath = "config.toml"
	}

	config, err := LoadConfigFromFile(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("Using config:", configPath)

	proxy := NewProxy(config)
	proxy.Start()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Proxy is running. Press Ctrl+C to stop.")
	<-sigChan

	log.Println("Received shutdown signal, stopping proxy...")
	proxy.Stop()
}
