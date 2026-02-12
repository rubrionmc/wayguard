package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"wayguard/k8s"
)

func main() {
	config, err := LoadConfigFromFile("config.toml")
	if err != nil {
		log.Fatalf("Error loading config Proxy failed to start: %v", err)
	}

	if !k8s.IsInClusterEnv() {
		log.Fatal("Instance is not in a client cluster environment KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT will be null")
	}

	client, err := k8s.NewInClusterClient()
	if err != nil {
		log.Fatalf("Error while creating client Helper: %v", err)
	}

	proxy := NewProxy(config, client)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := proxy.Start(); err != nil {
			log.Fatalf("Proxy failed: %v", err)
		}
	}()

	<-sigChan
	log.Println("Shutting down proxy...")
	proxy.Stop()
	log.Println("Proxy stopped")
}
