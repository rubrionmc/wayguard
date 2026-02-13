package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	"wayguard/k8s"
)

const (
	retryInterval = 2 * time.Second
	logInterval   = 30 * time.Second
)

func main() {

	log.Printf("Starting proxy with config file: %s", "config.toml")

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

	stopChan := make(chan struct{})

	go func() {
		var lastLog time.Time

		for {
			err := proxy.Start()
			if err != nil {
				now := time.Now()

				if lastLog.IsZero() || now.Sub(lastLog) >= logInterval {
					log.Printf("Proxy failed: %v", err)
					log.Printf("Retrying every %s (logging every %s)", retryInterval, logInterval)
					lastLog = now
				}

				select {
				case <-time.After(retryInterval):
					continue
				case <-stopChan:
					return
				}
			}

			select {
			case <-stopChan:
				return
			}
		}
	}()

	<-sigChan
	log.Println("Shutting down proxy...")
	close(stopChan)
	proxy.Stop()
	log.Println("Proxy stopped")
}
