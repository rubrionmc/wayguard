package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"wayguard/k8s"
)

type Proxy struct {
	config            *Config
	k8sClient         *k8s.Client
	currentBackend    string
	currentBackendIP  string
	primaryInfo       *k8s.BackendInfo
	fallbackInfo      *k8s.BackendInfo
	backendMutex      sync.RWMutex
	stopChan          chan struct{}
	healthTicker      *time.Ticker
	discoveryTicker   *time.Ticker
	lastSwitchTime    time.Time
	switchCooldown    time.Duration
	discovering       bool
	discoveryMutex    sync.Mutex
	healthFailures    int
	maxHealthFailures int
	lastFailureTime   time.Time
	gracePeriod       time.Duration
}

func NewProxy(config *Config, k8sClient *k8s.Client) *Proxy {
	return &Proxy{
		config:            config,
		k8sClient:         k8sClient,
		stopChan:          make(chan struct{}),
		switchCooldown:    5 * time.Second,  // Reduced cooldown for better responsiveness
		maxHealthFailures: 3,                // Force service fallback after 3 consecutive health failures
		gracePeriod:       30 * time.Second, // Wait 30s before retrying after all backends fail
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
			log.Printf("Failed to close listener on %s: %v", p.config.Server.Listen, err)
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

	backendAddr, backendType := p.getBackendAddress()
	if backendAddr == "" {
		log.Printf("No available backend for connection from %s", clientConn.RemoteAddr())
		return
	}

	dialer := &net.Dialer{
		Timeout: time.Duration(p.config.Timings.BackendDial),
	}

	backendConn, err := dialer.Dial("tcp", backendAddr)
	if err != nil {
		log.Printf("Failed to connect to backend %s (%s): %v", backendAddr, backendType, err)
		p.markBackendUnhealthy()
		return
	}
	defer func(backendConn net.Conn) {
		err := backendConn.Close()
		if err != nil {
			log.Printf("Failed to close backend connection: %v", err)
		}
	}(backendConn)

	log.Printf("Proxying connection %s -> %s (%s)", clientConn.RemoteAddr(), backendAddr, backendType)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, err := io.Copy(backendConn, clientConn)
		if err != nil {
			log.Printf("Failed to copy client to backend: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		_, err := io.Copy(clientConn, backendConn)
		if err != nil {
			log.Printf("Failed to copy backend to client: %v", err)
		}
	}()

	wg.Wait()
}

// getBackendAddress returns the backend address and type
// Prefers service name, falls back to IP if service is not available
// Falls back to IP if DNS resolution fails
func (p *Proxy) getBackendAddress() (string, string) {
	p.backendMutex.RLock()
	defer p.backendMutex.RUnlock()

	if p.currentBackend == "" {
		return "", ""
	}

	var service string
	var port int

	if p.currentBackend == "primary" {
		service = p.config.Backends.Primary.Service
		port = p.config.Backends.Primary.Port
	} else {
		service = p.config.Backends.Fallback.Service
		port = p.config.Backends.Fallback.Port
	}

	// Prefer service name if available
	if service != "" {
		serviceAddr := fmt.Sprintf("%s:%d", service, port)

		// Test if service name resolves
		if p.testDNSResolution(service) {
			return serviceAddr, p.currentBackend
		} else {
			log.Printf("DNS resolution failed for %s, falling back to IP", service)
		}
	}

	// Fallback to IP
	if p.currentBackendIP != "" {
		return fmt.Sprintf("%s:%d", p.currentBackendIP, port), p.currentBackend
	}

	return "", ""
}

func (p *Proxy) testDNSResolution(hostname string) bool {
	// Simple DNS resolution test
	_, err := net.LookupHost(hostname)
	return err == nil
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
	// Prevent multiple discovery calls from running simultaneously
	p.discoveryMutex.Lock()
	if p.discovering {
		p.discoveryMutex.Unlock()
		log.Printf("Discovery already in progress, skipping")
		return nil
	}
	p.discovering = true
	p.discoveryMutex.Unlock()

	defer func() {
		p.discoveryMutex.Lock()
		p.discovering = false
		p.discoveryMutex.Unlock()
	}()

	if !p.config.Discovery.Enabled {
		return nil
	}

	namespace := p.config.Discovery.Namespace

	// Get primary backend info
	primaryInfo, err := p.k8sClient.GetBackendInfo(namespace, p.config.Backends.Primary.Type, p.config.Backends.Primary.Service)
	if err != nil {
		log.Printf("Error discovering primary backends: %v", err)
	} else {
		p.backendMutex.Lock()
		p.primaryInfo = primaryInfo
		p.backendMutex.Unlock()

		if len(primaryInfo.Pods) > 0 {
			readyPods := p.getReadyPods(primaryInfo.Pods)
			if len(readyPods) > 0 {
				p.setBackend("primary", readyPods[0].IP)
				log.Printf("Discovered %d primary pods (%d ready), selected: %s", len(primaryInfo.Pods), len(readyPods), readyPods[0].Name)
				return nil
			}
		}
	}

	// Get fallback backend info
	fallbackInfo, err := p.k8sClient.GetBackendInfo(namespace, p.config.Backends.Fallback.Type, p.config.Backends.Fallback.Service)
	if err != nil {
		return fmt.Errorf("error discovering fallback backends: %w", err)
	}

	p.backendMutex.Lock()
	p.fallbackInfo = fallbackInfo
	p.backendMutex.Unlock()

	if len(fallbackInfo.Pods) > 0 {
		readyPods := p.getReadyPods(fallbackInfo.Pods)
		if len(readyPods) > 0 {
			p.setBackend("fallback", readyPods[0].IP)
			log.Printf("No primary backends available, using fallback: %s (%d pods, %d ready)", readyPods[0].Name, len(fallbackInfo.Pods), len(readyPods))
			return nil
		}
	}

	return fmt.Errorf("no backends available")
}

func (p *Proxy) getReadyPods(pods []k8s.PodInfo) []k8s.PodInfo {
	var readyPods []k8s.PodInfo
	for _, pod := range pods {
		if pod.Ready {
			readyPods = append(readyPods, pod)
		}
	}
	return readyPods
}

func (p *Proxy) setBackend(backendType, ip string) {
	p.backendMutex.Lock()
	defer p.backendMutex.Unlock()

	// Allow switching if clearing backend or if cooldown has passed
	if backendType != "" && time.Since(p.lastSwitchTime) < p.switchCooldown && p.currentBackend != "" {
		log.Printf("Backend switch cooldown active, skipping switch from %s to %s", p.currentBackend, backendType)
		return
	}

	p.currentBackend = backendType
	p.currentBackendIP = ip
	p.lastSwitchTime = time.Now()

	if backendType != "" {
		log.Printf("Switched to %s backend: %s", backendType, ip)
	} else {
		log.Printf("Cleared backend (no backend available)")
	}
}

func (p *Proxy) startHealthChecking() {
	interval := time.Duration(p.config.Timings.HealthcheckInterval)
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
	p.backendMutex.RLock()
	currentBackend := p.currentBackend
	lastFailure := p.lastFailureTime
	p.backendMutex.RUnlock()

	// If we're in grace period after all backends failed, skip health checks
	if currentBackend == "" && time.Since(lastFailure) < p.gracePeriod {
		return
	}

	if currentBackend == "" {
		if err := p.discoverBackends(); err != nil {
			log.Printf("Health check: Failed to discover backends: %v", err)
			p.backendMutex.Lock()
			p.lastFailureTime = time.Now()
			p.backendMutex.Unlock()
		}
		return
	}

	if !p.isBackendHealthy() {
		p.backendMutex.Lock()
		p.healthFailures++
		currentBackendType := p.currentBackend
		p.backendMutex.Unlock()

		log.Printf("Health check failed for current backend %s (failure %d/%d)", currentBackendType, p.healthFailures, p.maxHealthFailures)

		// Force service fallback after multiple consecutive failures
		if p.healthFailures >= p.maxHealthFailures {
			log.Printf("Too many consecutive health failures (%d), forcing service fallback", p.healthFailures)
			p.forceServiceFallback()
			p.healthFailures = 0 // Reset counter
		} else {
			p.markBackendUnhealthy()
		}
	} else {
		// Reset failure counter on successful health check
		p.backendMutex.Lock()
		p.healthFailures = 0
		p.backendMutex.Unlock()
	}
}

// isBackendHealthy checks the health of the current backend
// Uses service name if available, falls back to IP
func (p *Proxy) isBackendHealthy() bool {
	backendAddr, backendType := p.getBackendAddress()
	if backendAddr == "" {
		return false
	}

	dialer := &net.Dialer{
		Timeout: time.Duration(p.config.Timings.HealthcheckDial),
	}

	conn, err := dialer.Dial("tcp", backendAddr)
	if err != nil {
		// Check if it's a DNS error
		if strings.Contains(err.Error(), "lookup") || strings.Contains(err.Error(), "timeout") {
			log.Printf("Health check failed for %s backend at %s: DNS resolution error - %v", backendType, backendAddr, err)
		} else {
			log.Printf("Health check failed for %s backend at %s: %v", backendType, backendAddr, err)
		}
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

	log.Printf("Marking backend %s as unhealthy, attempting to find replacement", currentBackend)

	// Clear backend first to allow immediate switching
	p.setBackend("", "")

	if err := p.discoverBackends(); err != nil {
		log.Printf("Failed to discover new backend after marking %s unhealthy: %v", currentBackend, err)

		// If no backends are available, try to use the service name directly as a last resort
		p.forceServiceFallback()
	} else {
		// Discovery succeeded but still might need service fallback if health checks keep failing
		p.backendMutex.RLock()
		newBackend := p.currentBackend
		p.backendMutex.RUnlock()

		if newBackend == "" {
			p.forceServiceFallback()
		}
	}
}

func (p *Proxy) forceServiceFallback() {
	// Try fallback service first
	if p.config.Backends.Fallback.Service != "" {
		fallbackAddr := fmt.Sprintf("%s:%d", p.config.Backends.Fallback.Service, p.config.Backends.Fallback.Port)
		if p.testConnection(fallbackAddr) {
			p.setBackend("fallback", "") // Use empty IP to force service name usage
			log.Printf("Forced fallback to service: %s", fallbackAddr)
			return
		} else {
			log.Printf("Service fallback failed for %s: %v", fallbackAddr, "connection failed")
		}
	}

	// Try primary service as fallback
	if p.config.Backends.Primary.Service != "" {
		primaryAddr := fmt.Sprintf("%s:%d", p.config.Backends.Primary.Service, p.config.Backends.Primary.Port)
		if p.testConnection(primaryAddr) {
			p.setBackend("primary", "") // Use empty IP to force service name usage
			log.Printf("Forced fallback to primary service: %s", primaryAddr)
			return
		} else {
			log.Printf("Service fallback failed for %s: %v", primaryAddr, "connection failed")
		}
	}

	// All service fallback attempts failed - try to use pod IPs directly
	p.backendMutex.RLock()
	primaryInfo := p.primaryInfo
	fallbackInfo := p.fallbackInfo
	p.backendMutex.RUnlock()

	// Try fallback pod IPs
	if fallbackInfo != nil && len(fallbackInfo.Pods) > 0 {
		for _, pod := range fallbackInfo.Pods {
			if pod.Ready {
				podAddr := fmt.Sprintf("%s:%d", pod.IP, p.config.Backends.Fallback.Port)
				if p.testConnection(podAddr) {
					p.setBackend("fallback", pod.IP)
					log.Printf("Forced fallback to pod IP: %s", podAddr)
					return
				}
			}
		}
	}

	// Try primary pod IPs
	if primaryInfo != nil && len(primaryInfo.Pods) > 0 {
		for _, pod := range primaryInfo.Pods {
			if pod.Ready {
				podAddr := fmt.Sprintf("%s:%d", pod.IP, p.config.Backends.Primary.Port)
				if p.testConnection(podAddr) {
					p.setBackend("primary", pod.IP)
					log.Printf("Forced fallback to primary pod IP: %s", podAddr)
					return
				}
			}
		}
	}

	log.Printf("All fallback attempts failed, no backend available - entering grace period")
	p.backendMutex.Lock()
	p.lastFailureTime = time.Now()
	p.backendMutex.Unlock()
}

func (p *Proxy) testConnection(addr string) bool {
	dialer := &net.Dialer{
		Timeout: time.Duration(p.config.Timings.HealthcheckDial),
	}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return false
	}

	err = conn.Close()
	if err != nil {
		log.Printf("Failed to close test connection: %v", err)
		return false
	}

	return true
}

func (p *Proxy) startDiscovery() {
	interval := time.Duration(p.config.Timings.DiscoveryInterval)
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

	// Only try to switch to primary if currently using fallback
	if currentBackend == "fallback" {
		namespace := p.config.Discovery.Namespace

		// Refresh primary backend info
		primaryInfo, err := p.k8sClient.GetBackendInfo(namespace, p.config.Backends.Primary.Type, p.config.Backends.Primary.Service)
		if err != nil {
			log.Printf("Discovery: Error checking primary backends: %v", err)
			return
		}

		p.backendMutex.Lock()
		p.primaryInfo = primaryInfo
		p.backendMutex.Unlock()

		if len(primaryInfo.Pods) > 0 {
			readyPods := p.getReadyPods(primaryInfo.Pods)
			if len(readyPods) > 0 {
				// Check if primary service is healthy using service name first, then IP
				var checkAddr string
				if p.config.Backends.Primary.Service != "" {
					checkAddr = fmt.Sprintf("%s:%d", p.config.Backends.Primary.Service, p.config.Backends.Primary.Port)
				} else {
					checkAddr = fmt.Sprintf("%s:%d", readyPods[0].IP, p.config.Backends.Primary.Port)
				}

				dialer := &net.Dialer{
					Timeout: time.Duration(p.config.Timings.HealthcheckDial),
				}

				if conn, err := dialer.Dial("tcp", checkAddr); err == nil {
					err := conn.Close()
					if err != nil {
						log.Printf("Failed to close backend connection: %v", err)
						return
					}
					log.Printf("Discovery: Primary backend is now available, switching from fallback")
					p.setBackend("primary", readyPods[0].IP)
				} else {
					log.Printf("Discovery: Primary backend found but health check failed: %v", err)
				}
			} else {
				log.Printf("Discovery: Primary backend pods found but none are ready")
			}
		} else {
			log.Printf("Discovery: No primary backend pods found")
		}
	}
}
