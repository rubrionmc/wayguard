/*
 * This file is part of the Rubrion Group.
 *
 * Licensed under the Rubrion Public License (RPL), Version 1, 2026.
 * You may not use this file except in compliance with the License.
 *
 * License:
 * https://rubrionmc.github.io/.github/licensens/RUBRION_PUBLIC_LICENSE
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 */
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
	config    *Config
	backends  map[BackendType]*Backend
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	semaphore chan struct{}
}

func NewProxy(config *Config) *Proxy {
	ctx, cancel := context.WithCancel(context.Background())

	maxConn := config.Limitations.MaxConnections
	if maxConn <= 0 {
		maxConn = 1024
	}

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
		config:    config,
		backends:  backends,
		ctx:       ctx,
		cancel:    cancel,
		semaphore: make(chan struct{}, maxConn),
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

	ticker := time.NewTicker(p.config.Timings.HealthcheckInterval)
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

	ticker := time.NewTicker(p.config.Timings.LogRateLimitInterval)
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
	p.mu.RLock()
	types := make([]BackendType, 0, len(p.backends))
	for typ := range p.backends {
		types = append(types, typ)
	}
	p.mu.RUnlock()

	for _, typ := range types {
		p.checkBackend(typ)
	}
}

func (p *Proxy) checkBackend(typ BackendType) {
	backend := p.getBackend(typ)
	if backend == nil || backend.Address == "" {
		p.setBackendHealth(typ, false)
		return
	}

	timeout := p.config.Timings.HealthcheckDial
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

	handshake, err := createHandshakePacket(host, portStr, 0x01)
	if err != nil {
		log.Printf("Error creating handshake packet: %s", err)
		return false
	}

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

	if packetLength <= 0 || packetLength > p.config.Limitations.MaxBytesPerPacket {
		return false
	}

	response := make([]byte, packetLength)
	if _, err := io.ReadFull(conn, response); err != nil {
		return false
	}

	return true
}

func createHandshakePacket(host, port string, nextState byte) ([]byte, error) {
	var data []byte

	data = appendVarInt(data, 0x00)
	data = appendVarInt(data, 47)
	data = appendVarInt(data, int32(len(host)))
	data = append(data, []byte(host)...)

	var portNum int
	if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil {
		return nil, fmt.Errorf("error parsing port number %q: %w", port, err)
	}

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(portNum))
	data = append(data, portBytes...)

	data = appendVarInt(data, int32(nextState))

	packet := appendVarInt([]byte{}, int32(len(data)))
	packet = append(packet, data...)

	return packet, nil
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

	log.Println("Backend Status Report:")
	for _, backend := range p.backends {
		status := "UNHEALTHY"
		if backend.Healthy {
			status = "HEALTHY"
		}
		log.Printf("%s - %s (%s)", backend.Name, status, backend.Address)
	}
}

func (p *Proxy) discoveryLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.Timings.DiscoveryInterval)
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

	p.mu.RLock()
	snapshot := make(map[BackendType]Backend, len(p.backends))
	for typ, backend := range p.backends {
		snapshot[typ] = *backend
	}
	p.mu.RUnlock()

	for typ, backend := range snapshot {
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
	defer func(listener net.Listener) {
		err := listener.Close()
		if err != nil {
			log.Printf("Error closing listener: %v", err)
		}
	}(listener)

	log.Printf("Listening on %s", listenAddr)

	go func() {
		<-p.ctx.Done()
		err := listener.Close()
		if err != nil {
			log.Printf("Error closing listener: %v", err)
		}
	}()

	maxConn := p.config.Limitations.MaxConnections
	if maxConn <= 0 {
		maxConn = 1024
	}

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

		select {
		case p.semaphore <- struct{}{}:
			go p.handleConnection(conn)
		default:
			log.Printf("Connection limit reached (%d), rejecting %s", maxConn, conn.RemoteAddr())
			err := conn.Close()
			if err != nil {
				log.Printf("Error closing rejected connection: %v", err)
				continue
			}
		}
	}
}

type limitedConn struct {
	net.Conn
	limits LimitationsConfig

	windowStart     time.Time
	bytesThisWindow int32
	pktsThisWindow  int32
	mu              sync.Mutex
}

func newLimitedConn(conn net.Conn, limits LimitationsConfig) *limitedConn {
	return &limitedConn{
		Conn:        conn,
		limits:      limits,
		windowStart: time.Now(),
	}
}

func (lc *limitedConn) Read(b []byte) (int, error) {
	maxPkt := lc.limits.MaxBytesPerPacket
	var reader io.Reader = lc.Conn
	if maxPkt > 0 {
		reader = io.LimitReader(lc.Conn, int64(maxPkt))
	}

	n, err := reader.Read(b)
	if n <= 0 || err != nil {
		return n, err
	}

	lc.mu.Lock()
	defer lc.mu.Unlock()

	now := time.Now()
	if now.Sub(lc.windowStart) >= time.Second {
		lc.windowStart = now
		lc.bytesThisWindow = 0
		lc.pktsThisWindow = 0
	}

	lc.bytesThisWindow += int32(n)
	lc.pktsThisWindow++

	if lc.limits.MaxBytesPerSecond > 0 && lc.bytesThisWindow > lc.limits.MaxBytesPerSecond {
		log.Printf("Rate limit exceeded (bytes/s) for %s — closing connection", lc.Conn.RemoteAddr())
		_ = lc.Conn.Close()
		return n, fmt.Errorf("rate limit exceeded: bytes per second")
	}

	if lc.limits.MaxPacketsPerSecond > 0 && lc.pktsThisWindow > lc.limits.MaxPacketsPerSecond {
		log.Printf("Rate limit exceeded (packets/s) for %s — closing connection", lc.Conn.RemoteAddr())
		_ = lc.Conn.Close()
		return n, fmt.Errorf("rate limit exceeded: packets per second")
	}

	return n, nil
}

func (p *Proxy) handleConnection(clientConn net.Conn) {
	defer func() { <-p.semaphore }()
	defer func(clientConn net.Conn) {
		err := clientConn.Close()
		if err != nil {
			log.Printf("Error closing client connection: %s", err)
		}
	}(clientConn)

	backend := p.GetHealthyBackend()
	if backend == nil {
		log.Printf("No healthy backend available for connection from %s", clientConn.RemoteAddr())
		return
	}

	backendConn, err := net.DialTimeout("tcp", backend.Address, p.config.Timings.BackendDial)
	if err != nil {
		log.Printf("Failed to connect to backend %s: %v", backend.Address, err)
		return
	}
	defer func(backendConn net.Conn) {
		err := backendConn.Close()
		if err != nil {
			log.Printf("Error closing backend connection: %s", err)
		}
	}(backendConn)

	log.Printf("Proxying connection from %s to %s (%s)",
		clientConn.RemoteAddr(), backend.Name, backend.Address)

	limited := newLimitedConn(clientConn, p.config.Limitations)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if _, err := io.Copy(backendConn, limited); err != nil {
			log.Printf("Error copying data from client to backend: %v", err)
		}
		if tc, ok := backendConn.(*net.TCPConn); ok {
			err := tc.CloseWrite()
			if err != nil {
				log.Printf("Error closing backend connection write side: %s", err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		if _, err := io.Copy(clientConn, backendConn); err != nil {
			log.Printf("Error copying data from backend to client: %v", err)
		}
		if tc, ok := clientConn.(*net.TCPConn); ok {
			err := tc.CloseWrite()
			if err != nil {
				log.Printf("Error closing backend connection write side: %s", err)
				return
			}
		}
	}()

	wg.Wait()
}
