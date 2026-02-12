package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"wayguard/k8s"
)

type Proxy struct {
	config           *Config
	k8sClient        *k8s.Client
	currentBackend   string
	currentBackendIP string
	backendMutex     sync.RWMutex
	stopChan         chan struct{}
	healthTicker     *time.Ticker
	discoveryTicker  *time.Ticker
}

func NewProxy(config *Config, k8sClient *k8s.Client) *Proxy {
	return &Proxy{
		config:    config,
		k8sClient: k8sClient,
		stopChan:  make(chan struct{}),
	}
}

func (p *Proxy) Start() error {
	if err := p.discoverBackends(); err != nil {
		return fmt.Errorf("initial backend discovery failed: %w", err)
	}

	p.startHealthChecking()

	if p.config.Discovery.Enabled {
		p.startDiscovery()
	}

	listener, err := net.Listen("tcp", p.config.Server.Listen)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.config.Server.Listen, err)
	}
	defer func(listener net.Listener) {
		err := listener.Close()
		if err != nil {
			log.Printf("failed to close listener on %s: %w", p.config.Server.Listen, err)
		}
	}(listener)

	log.Printf("Proxy server started on %s", p.config.Server.Listen)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-p.stopChan:
				return nil
			default:
				log.Printf("Error accepting connection: %v", err)
				continue
			}
		}

		go p.handleConnection(conn)
	}
}

func (p *Proxy) Stop() {
	close(p.stopChan)

	if p.healthTicker != nil {
		p.healthTicker.Stop()
	}

	if p.discoveryTicker != nil {
		p.discoveryTicker.Stop()
	}
}

func (p *Proxy) handleConnection(clientConn net.Conn) {
	defer func(clientConn net.Conn) {
		err := clientConn.Close()
		if err != nil {
			log.Printf("Failed to close client connection: %v", err)
		}
	}(clientConn)

	backendIP := p.getCurrentBackend()
	if backendIP == "" {
		log.Printf("No available backend for connection from %s", clientConn.RemoteAddr())
		return
	}

	backendAddr := fmt.Sprintf("%s:%d", backendIP, p.getBackendPort())

	dialer := &net.Dialer{
		Timeout: time.Duration(p.config.Timeouts.BackendDial),
	}

	backendConn, err := dialer.Dial("tcp", backendAddr)
	if err != nil {
		log.Printf("Failed to connect to backend %s: %v", backendAddr, err)
		p.markBackendUnhealthy()
		return
	}
	defer func(backendConn net.Conn) {
		err := backendConn.Close()
		if err != nil {
			log.Printf("Failed to close backend connection: %v", err)
		}
	}(backendConn)

	log.Printf("Proxying connection %s -> %s", clientConn.RemoteAddr(), backendAddr)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, err := io.Copy(backendConn, clientConn)
		if err != nil {
			log.Printf("Failed to copy backend connection to client: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		_, err := io.Copy(clientConn, backendConn)
		if err != nil {
			log.Printf("Failed to copy backend connection to client: %v", err)
		}
	}()

	wg.Wait()
}

func (p *Proxy) getCurrentBackend() string {
	p.backendMutex.RLock()
	defer p.backendMutex.RUnlock()
	return p.currentBackendIP
}

func (p *Proxy) getBackendPort() int {
	p.backendMutex.RLock()
	defer p.backendMutex.RUnlock()

	if p.currentBackend == "primary" {
		return p.config.Backends.Primary.Port
	}
	return p.config.Backends.Fallback.Port
}

func (p *Proxy) discoverBackends() error {
	if !p.config.Discovery.Enabled {
		return nil
	}

	namespace := p.config.Discovery.Namespace

	primaryIPs, err := p.k8sClient.GetPodIPsByLabel(namespace, p.config.Backends.Primary.Type)
	if err != nil {
		log.Printf("Error discovering primary backends: %v", err)
	} else if len(primaryIPs) > 0 {
		p.setBackend("primary", primaryIPs[0])
		log.Printf("Discovered %d primary backends, selected: %s", len(primaryIPs), primaryIPs[0])
		return nil
	}

	fallbackIPs, err := p.k8sClient.GetPodIPsByLabel(namespace, p.config.Backends.Fallback.Type)
	if err != nil {
		return fmt.Errorf("error discovering fallback backends: %w", err)
	}

	if len(fallbackIPs) > 0 {
		p.setBackend("fallback", fallbackIPs[0])
		log.Printf("No primary backends available, using fallback: %s", fallbackIPs[0])
		return nil
	}

	return fmt.Errorf("no backends available")
}

func (p *Proxy) setBackend(backendType, ip string) {
	p.backendMutex.Lock()
	defer p.backendMutex.Unlock()

	p.currentBackend = backendType
	p.currentBackendIP = ip
	log.Printf("Switched to %s backend: %s", backendType, ip)
}

func (p *Proxy) startHealthChecking() {
	interval := time.Duration(p.config.Timeouts.HealthcheckInterval)
	p.healthTicker = time.NewTicker(interval)

	go func() {
		for {
			select {
			case <-p.stopChan:
				return
			case <-p.healthTicker.C:
				p.performHealthCheck()
			}
		}
	}()
}

func (p *Proxy) performHealthCheck() {
	currentIP := p.getCurrentBackend()
	if currentIP == "" {
		if err := p.discoverBackends(); err != nil {
			log.Printf("Health check: Failed to discover backends: %v", err)
		}
		return
	}

	if !p.isBackendHealthy(currentIP) {
		log.Printf("Health check failed for current backend %s", currentIP)
		p.markBackendUnhealthy()
	}
}

func (p *Proxy) isBackendHealthy(ip string) bool {
	backendAddr := fmt.Sprintf("%s:%d", ip, p.getBackendPort())

	dialer := &net.Dialer{
		Timeout: time.Duration(p.config.Timeouts.HealthcheckDial),
	}

	conn, err := dialer.Dial("tcp", backendAddr)
	if err != nil {
		return false
	}

	err = conn.Close()
	if err != nil {
		log.Printf("Failed to close backend connection: %v", err)
		return false
	}

	return true
}

func (p *Proxy) markBackendUnhealthy() {
	p.backendMutex.Lock()
	currentBackend := p.currentBackend
	p.backendMutex.Unlock()

	p.setBackend("", "")

	if err := p.discoverBackends(); err != nil {
		log.Printf("Failed to discover new backend after marking %s unhealthy: %v", currentBackend, err)
	}
}

func (p *Proxy) startDiscovery() {
	interval := time.Duration(p.config.Timeouts.DiscoveryInterval)
	p.discoveryTicker = time.NewTicker(interval)

	go func() {
		for {
			select {
			case <-p.stopChan:
				return
			case <-p.discoveryTicker.C:
				p.performDiscovery()
			}
		}
	}()
}

func (p *Proxy) performDiscovery() {
	p.backendMutex.RLock()
	currentBackend := p.currentBackend
	p.backendMutex.RUnlock()

	if currentBackend == "fallback" {
		namespace := p.config.Discovery.Namespace
		primaryIPs, err := p.k8sClient.GetPodIPsByLabel(namespace, p.config.Backends.Primary.Type)
		if err != nil {
			log.Printf("Discovery: Error checking primary backends: %v", err)
			return
		}

		if len(primaryIPs) > 0 {
			primaryAddr := fmt.Sprintf("%s:%d", primaryIPs[0], p.config.Backends.Primary.Port)
			dialer := &net.Dialer{
				Timeout: time.Duration(p.config.Timeouts.HealthcheckDial),
			}

			if conn, err := dialer.Dial("tcp", primaryAddr); err == nil {
				err := conn.Close()
				if err != nil {
					log.Printf("Failed to close backend connection: %v", err)
					return
				}
				log.Printf("Discovery: Primary backend is now available, switching from fallback")
				p.setBackend("primary", primaryIPs[0])
			}
		}
	}
}
