package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type BackendType int

const (
	BackendPrimary BackendType = iota
	BackendFallback
)

type Backend struct {
	Type    BackendType
	Name    string
	Port    int
	Address string
	Healthy bool
}

type Proxy struct {
	config   *Config
	backends map[BackendType]*Backend
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewProxy(config *Config) *Proxy {
	ctx, cancel := context.WithCancel(context.Background())

	backends := map[BackendType]*Backend{
		BackendPrimary: {
			Type:    BackendPrimary,
			Name:    config.Backends.Primary.Name,
			Port:    config.Backends.Primary.Port,
			Healthy: false,
		},
		BackendFallback: {
			Type:    BackendFallback,
			Name:    config.Backends.Fallback.Name,
			Port:    config.Backends.Fallback.Port,
			Healthy: false,
		},
	}

	return &Proxy{
		config:   config,
		backends: backends,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (p *Proxy) Start() {
	p.discoverBackends()
	p.checkAllBackends()
	p.printBackendStatus()

	p.wg.Add(1)
	go p.healthCheckLoop()

	p.wg.Add(1)
	go p.statusLoop()

	if p.config.Discovery.Namespace != "" {
		p.wg.Add(1)
		go p.discoveryLoop()
	}

	p.wg.Add(1)
	go p.startTCPListener()
}

func (p *Proxy) Stop() {
	log.Println("Stopping proxy...")
	p.cancel()
	p.wg.Wait()
	log.Println("Proxy stopped")
}

func (p *Proxy) getBackend(typ BackendType) *Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if b, ok := p.backends[typ]; ok {
		return &Backend{
			Type:    b.Type,
			Name:    b.Name,
			Port:    b.Port,
			Address: b.Address,
			Healthy: b.Healthy,
		}
	}
	return nil
}

func (p *Proxy) setBackendHealth(typ BackendType, healthy bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if backend, ok := p.backends[typ]; ok {
		backend.Healthy = healthy
	}
}

func (p *Proxy) setBackendAddress(typ BackendType, address string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if backend, ok := p.backends[typ]; ok {
		backend.Address = address
	}
}

func (p *Proxy) healthCheckLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(time.Duration(p.config.Timings.HealthcheckInterval) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.checkAllBackends()
		}
	}
}

func (p *Proxy) statusLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(time.Duration(p.config.Timings.LogRateLimitInterval) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.printBackendStatus()
		}
	}
}

func (p *Proxy) checkAllBackends() {
	for typ := range p.backends {
		p.checkBackend(typ)
	}
}

func (p *Proxy) checkBackend(typ BackendType) {
	backend := p.getBackend(typ)
	if backend == nil || backend.Address == "" {
		p.setBackendHealth(typ, false)
		return
	}

	timeout := time.Duration(p.config.Timings.HealthcheckDial) * time.Millisecond
	healthy := p.pingMinecraftServer(backend.Address, timeout)

	wasHealthy := backend.Healthy

	if !healthy {
		p.setBackendHealth(typ, false)
		if wasHealthy {
			log.Printf("%s (%s) - UNHEALTHY", backend.Name, backend.Address)
		}
		return
	}

	if !wasHealthy {
		log.Printf("%s (%s) - HEALTHY", backend.Name, backend.Address)
	}
	p.setBackendHealth(typ, true)
}

func (p *Proxy) pingMinecraftServer(address string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {
			log.Printf("Error closing connection: %s", err)
		}
	}(conn)

	err = conn.SetDeadline(time.Now().Add(timeout * 3))
	if err != nil {
		log.Printf("Error setting deadline: %s", err)
		return false
	}

	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}

	handshake := createHandshakePacket(host, portStr, 0x01)
	if _, err := conn.Write(handshake); err != nil {
		return false
	}

	statusRequest := []byte{0x01, 0x00}
	if _, err := conn.Write(statusRequest); err != nil {
		return false
	}

	packetLength, err := readVarInt(conn)
	if err != nil {
		return false
	}

	if packetLength <= 0 || packetLength > 32767 {
		return false
	}

	response := make([]byte, packetLength)
	if _, err := io.ReadFull(conn, response); err != nil {
		return false
	}

	return true
}

func createHandshakePacket(host, port string, nextState byte) []byte {
	var data []byte

	data = appendVarInt(data, 0x00)
	data = appendVarInt(data, 47)
	data = appendVarInt(data, int32(len(host)))
	data = append(data, []byte(host)...)

	portNum := 25575
	_, err := fmt.Sscanf(port, "%d", &portNum)
	if err != nil {
		log.Printf("Error parsing port number: %s", err)
		return nil
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(portNum))
	data = append(data, portBytes...)

	data = appendVarInt(data, int32(nextState))

	packet := appendVarInt([]byte{}, int32(len(data)))
	packet = append(packet, data...)

	return packet
}

func appendVarInt(data []byte, value int32) []byte {
	for {
		temp := byte(value & 0x7F)
		value >>= 7
		if value != 0 {
			temp |= 0x80
		}
		data = append(data, temp)
		if value == 0 {
			break
		}
	}
	return data
}

func readVarInt(r io.Reader) (int32, error) {
	var result int32
	var numRead uint

	for {
		buf := make([]byte, 1)
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, err
		}

		value := buf[0]
		result |= int32(value&0x7F) << (7 * numRead)

		numRead++
		if numRead > 5 {
			return 0, fmt.Errorf("VarInt too big")
		}

		if (value & 0x80) == 0 {
			break
		}
	}

	return result, nil
}

func (p *Proxy) printBackendStatus() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	log.Println("=== Backend Status ===")
	for _, backend := range p.backends {
		status := "UNHEALTHY"
		if backend.Healthy {
			status = "HEALTHY"
		}
		log.Printf("%s - %s (%s)", backend.Name, status, backend.Address)
	}
	log.Println("======================")
}

func (p *Proxy) discoveryLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(time.Duration(p.config.Timings.DiscoveryInterval) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.discoverBackends()
		}
	}
}

func (p *Proxy) discoverBackends() {
	clusterDomain := p.config.Discovery.K8sClusterDomain
	namespace := p.config.Discovery.Namespace

	for typ, backend := range p.backends {
		serviceDNS := fmt.Sprintf("%s.%s.%s", backend.Name, namespace, clusterDomain)
		address := fmt.Sprintf("%s:%d", serviceDNS, backend.Port)

		currentBackend := p.getBackend(typ)
		if currentBackend == nil {
			log.Printf("Backend type %d not found in config", typ)
			continue
		}

		if currentBackend.Address != address {
			p.setBackendAddress(typ, address)
			log.Printf("Discovered backend %s at %s", backend.Name, address)
			p.checkBackend(typ)
		}
	}
}

func (p *Proxy) GetHealthyBackend() *Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if primary := p.backends[BackendPrimary]; primary != nil && primary.Healthy {
		return &Backend{
			Type:    primary.Type,
			Name:    primary.Name,
			Port:    primary.Port,
			Address: primary.Address,
			Healthy: primary.Healthy,
		}
	}

	if fallback := p.backends[BackendFallback]; fallback != nil && fallback.Healthy {
		return &Backend{
			Type:    fallback.Type,
			Name:    fallback.Name,
			Port:    fallback.Port,
			Address: fallback.Address,
			Healthy: fallback.Healthy,
		}
	}

	return nil
}

func (p *Proxy) startTCPListener() {
	defer p.wg.Done()

	listenAddr := p.config.Server.Listen
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to start listener on %s: %v", listenAddr, err)
	}
	defer listener.Close()

	log.Printf("Listening on %s", listenAddr)

	go func() {
		<-p.ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-p.ctx.Done():
				return
			default:
				log.Printf("Error accepting connection: %v", err)
				continue
			}
		}

		go p.handleConnection(conn)
	}
}

func (p *Proxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	backend := p.GetHealthyBackend()
	if backend == nil {
		log.Printf("No healthy backend available for connection from %s", clientConn.RemoteAddr())
		return
	}

	backendConn, err := net.DialTimeout("tcp", backend.Address,
		time.Duration(p.config.Timings.HealthcheckDial)*time.Millisecond)
	if err != nil {
		log.Printf("Failed to connect to backend %s: %v", backend.Address, err)
		return
	}
	defer backendConn.Close()

	log.Printf("Proxying connection from %s to %s (%s)",
		clientConn.RemoteAddr(), backend.Name, backend.Address)

	done := make(chan struct{}, 2)

	go func() {
		io.Copy(backendConn, clientConn)
		done <- struct{}{}
	}()

	go func() {
		io.Copy(clientConn, backendConn)
		done <- struct{}{}
	}()

	<-done
}
