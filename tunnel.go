package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tunnelLoopbackIPv4  = "127.0.0.233"
	tunnelLoopbackIPv6  = "::1"
	tunnelInternalIPv4  = "127.0.0.1"
	tunnelHTTPPort      = 80
	tunnelHTTPSPort     = 443
	tunnelMaxHeaderSize = 64 * 1024
)

type tunnelRoute struct {
	Hostname   string
	UpstreamIP string
	Family     ipFamily
}

type tunnelSnapshot struct {
	Active    bool
	Port      int
	RuleCount int
}

type localTunnelManager struct {
	mu       sync.RWMutex
	listener net.Listener
	port     int
	routes   map[string]tunnelRoute
	wg       sync.WaitGroup
	logf     func(string, ...interface{})
}

func newLocalTunnelManager(logf func(string, ...interface{})) *localTunnelManager {
	return &localTunnelManager{
		routes: make(map[string]tunnelRoute),
		logf:   logf,
	}
}

func (m *localTunnelManager) EnsureStarted(preferredPort int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listener != nil {
		if preferredPort <= 0 || preferredPort == m.port {
			return m.port, nil
		}
		m.stopLocked()
	}

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(preferredPort))
	if preferredPort <= 0 {
		address = "127.0.0.1:0"
	}

	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return 0, fmt.Errorf("start local tunnel on %s: %w", address, err)
	}

	m.listener = listener
	m.port = listener.Addr().(*net.TCPAddr).Port
	m.routes = make(map[string]tunnelRoute)
	m.wg.Add(1)
	go m.acceptLoop(listener)
	m.log("local tunnel listening on %s:%d for HTTP/HTTPS relay", tunnelInternalIPv4, m.port)
	return m.port, nil
}

func (m *localTunnelManager) SetRoutes(rows []resultRow, aliases map[string][]string) int {
	routes := make(map[string]tunnelRoute)
	for _, row := range rows {
		if strings.TrimSpace(row.Address) == "" {
			continue
		}
		hostnames := aliases[row.Domain]
		if len(hostnames) == 0 {
			hostnames = []string{row.Domain}
		}
		for _, hostname := range hostnames {
			normalized := strings.ToLower(strings.TrimSpace(hostname))
			if normalized == "" {
				continue
			}
			routes[normalized] = tunnelRoute{
				Hostname:   normalized,
				UpstreamIP: row.Address,
				Family:     familyFromIP(net.ParseIP(row.Address)),
			}
		}
	}

	m.mu.Lock()
	m.routes = routes
	m.mu.Unlock()
	m.log("local tunnel routes updated: %d hostnames", len(routes))
	return len(routes)
}

func (m *localTunnelManager) Snapshot() tunnelSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return tunnelSnapshot{
		Active:    m.listener != nil,
		Port:      m.port,
		RuleCount: len(m.routes),
	}
}

func (m *localTunnelManager) Stop() error {
	m.mu.Lock()
	m.stopLocked()
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}

func (m *localTunnelManager) stopLocked() {
	if m.listener != nil {
		_ = m.listener.Close()
		m.listener = nil
	}
	m.port = 0
	m.routes = make(map[string]tunnelRoute)
}

func (m *localTunnelManager) acceptLoop(listener net.Listener) {
	defer m.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if isClosedNetworkError(err) {
				return
			}
			m.log("local tunnel accept error: %v", err)
			return
		}
		go m.handleConn(conn)
	}
}

func (m *localTunnelManager) handleConn(client net.Conn) {
	defer client.Close()

	_ = client.SetReadDeadline(time.Now().Add(8 * time.Second))
	reader := bufio.NewReader(client)
	preface, err := readClientPreface(reader)
	_ = client.SetReadDeadline(time.Time{})
	if err != nil {
		m.log("local tunnel rejected connection: %v", err)
		return
	}

	route, ok := m.lookupRoute(preface.Hostname)
	if !ok {
		m.log("local tunnel missing route for %s", preface.Hostname)
		return
	}

	network := "tcp4"
	if route.Family == family6 {
		network = "tcp6"
	}

	upstreamAddr := net.JoinHostPort(route.UpstreamIP, strconv.Itoa(preface.UpstreamPort))
	upstream, err := (&net.Dialer{Timeout: 8 * time.Second}).Dial(network, upstreamAddr)
	if err != nil {
		m.log("local tunnel upstream dial failed for %s -> %s: %v", preface.Hostname, upstreamAddr, err)
		return
	}
	defer upstream.Close()

	if _, err := upstream.Write(preface.InitialBytes); err != nil {
		m.log("local tunnel upstream preface write failed for %s: %v", preface.Hostname, err)
		return
	}

	m.log("local tunnel connected %s %s -> %s", preface.Protocol, preface.Hostname, upstreamAddr)

	copyDone := make(chan struct{}, 2)
	go proxyStream(upstream, reader, copyDone)
	go proxyStream(client, upstream, copyDone)
	<-copyDone
}

func (m *localTunnelManager) lookupRoute(hostname string) (tunnelRoute, bool) {
	normalized := strings.ToLower(strings.TrimSpace(hostname))
	m.mu.RLock()
	defer m.mu.RUnlock()
	route, ok := m.routes[normalized]
	return route, ok
}

func (m *localTunnelManager) log(format string, args ...interface{}) {
	if m.logf != nil {
		m.logf(format, args...)
	}
}

func proxyStream(dst io.Writer, src io.Reader, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	select {
	case done <- struct{}{}:
	default:
	}
}

func isClosedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "use of closed network connection")
}

func readTLSClientHello(r io.Reader) ([]byte, string, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, "", fmt.Errorf("read tls record header: %w", err)
	}
	if header[0] != 22 {
		return nil, "", fmt.Errorf("unexpected tls record type %d", header[0])
	}

	recordLength := int(binary.BigEndian.Uint16(header[3:5]))
	if recordLength <= 0 || recordLength > 64*1024 {
		return nil, "", fmt.Errorf("invalid tls record length %d", recordLength)
	}

	body := make([]byte, recordLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, "", fmt.Errorf("read tls client hello body: %w", err)
	}

	packet := append(header, body...)
	hostname, err := parseServerNameFromTLSRecord(packet)
	if err != nil {
		return nil, "", err
	}
	return packet, hostname, nil
}

type clientPreface struct {
	InitialBytes []byte
	Hostname     string
	Protocol     string
	UpstreamPort int
}

func readClientPreface(reader *bufio.Reader) (clientPreface, error) {
	firstByte, err := reader.Peek(1)
	if err != nil {
		return clientPreface{}, fmt.Errorf("peek client preface: %w", err)
	}

	if len(firstByte) == 1 && firstByte[0] == 22 {
		record, hostname, err := readTLSClientHello(reader)
		if err != nil {
			return clientPreface{}, err
		}
		return clientPreface{
			InitialBytes: record,
			Hostname:     normalizeDomain(hostname),
			Protocol:     "https",
			UpstreamPort: tunnelHTTPSPort,
		}, nil
	}

	request, hostname, err := readHTTPRequest(reader)
	if err != nil {
		return clientPreface{}, err
	}
	return clientPreface{
		InitialBytes: request,
		Hostname:     hostname,
		Protocol:     "http",
		UpstreamPort: tunnelHTTPPort,
	}, nil
}

func readHTTPRequest(reader *bufio.Reader) ([]byte, string, error) {
	var header bytes.Buffer
	for header.Len() < tunnelMaxHeaderSize {
		line, err := reader.ReadBytes('\n')
		header.Write(line)
		if bytes.Contains(header.Bytes(), []byte("\r\n\r\n")) || bytes.Contains(header.Bytes(), []byte("\n\n")) {
			hostname, hostErr := parseHTTPHostHeader(header.Bytes())
			if hostErr != nil {
				return nil, "", hostErr
			}
			return header.Bytes(), hostname, nil
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, "", fmt.Errorf("read http request header: %w", err)
		}
	}
	return nil, "", fmt.Errorf("http request header too large or incomplete")
}

func parseHTTPHostHeader(header []byte) (string, error) {
	lines := strings.Split(strings.ReplaceAll(string(header), "\r\n", "\n"), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(strings.ToLower(line), "host:") {
			continue
		}
		value := normalizeDomain(strings.TrimSpace(line[len("host:"):]))
		if value == "" {
			break
		}
		return value, nil
	}
	return "", fmt.Errorf("http request did not include a valid Host header")
}

func parseServerNameFromTLSRecord(record []byte) (string, error) {
	if len(record) < 5+4 {
		return "", fmt.Errorf("tls record too short")
	}
	if record[0] != 22 {
		return "", fmt.Errorf("tls record is not a handshake")
	}

	body := record[5:]
	if len(body) < 4 || body[0] != 1 {
		return "", fmt.Errorf("tls handshake is not a client hello")
	}

	handshakeLength := int(body[1])<<16 | int(body[2])<<8 | int(body[3])
	if handshakeLength+4 > len(body) {
		return "", fmt.Errorf("truncated client hello")
	}

	offset := 4
	if offset+2+32 > len(body) {
		return "", fmt.Errorf("client hello missing version or random")
	}
	offset += 2 + 32

	if offset+1 > len(body) {
		return "", fmt.Errorf("client hello missing session id")
	}
	sessionIDLength := int(body[offset])
	offset++
	if offset+sessionIDLength > len(body) {
		return "", fmt.Errorf("client hello session id truncated")
	}
	offset += sessionIDLength

	if offset+2 > len(body) {
		return "", fmt.Errorf("client hello missing cipher suites")
	}
	cipherSuitesLength := int(binary.BigEndian.Uint16(body[offset : offset+2]))
	offset += 2
	if offset+cipherSuitesLength > len(body) {
		return "", fmt.Errorf("client hello cipher suites truncated")
	}
	offset += cipherSuitesLength

	if offset+1 > len(body) {
		return "", fmt.Errorf("client hello missing compression methods")
	}
	compressionMethodsLength := int(body[offset])
	offset++
	if offset+compressionMethodsLength > len(body) {
		return "", fmt.Errorf("client hello compression methods truncated")
	}
	offset += compressionMethodsLength

	if offset+2 > len(body) {
		return "", fmt.Errorf("client hello missing extensions")
	}
	extensionsLength := int(binary.BigEndian.Uint16(body[offset : offset+2]))
	offset += 2
	if offset+extensionsLength > len(body) {
		return "", fmt.Errorf("client hello extensions truncated")
	}
	extensionsEnd := offset + extensionsLength

	for offset+4 <= extensionsEnd {
		extensionType := binary.BigEndian.Uint16(body[offset : offset+2])
		extensionLength := int(binary.BigEndian.Uint16(body[offset+2 : offset+4]))
		offset += 4
		if offset+extensionLength > extensionsEnd {
			return "", fmt.Errorf("client hello extension truncated")
		}

		if extensionType == 0 {
			serverName, err := parseServerNameExtension(body[offset : offset+extensionLength])
			if err != nil {
				return "", err
			}
			if serverName == "" {
				return "", fmt.Errorf("client hello did not include a server name")
			}
			return serverName, nil
		}

		offset += extensionLength
	}

	return "", fmt.Errorf("client hello did not include an SNI extension")
}

func parseServerNameExtension(data []byte) (string, error) {
	if len(data) < 2 {
		return "", fmt.Errorf("server name extension too short")
	}
	listLength := int(binary.BigEndian.Uint16(data[:2]))
	if listLength == 0 || listLength+2 > len(data) {
		return "", fmt.Errorf("server name list truncated")
	}
	offset := 2
	listEnd := offset + listLength
	for offset+3 <= listEnd {
		nameType := data[offset]
		nameLength := int(binary.BigEndian.Uint16(data[offset+1 : offset+3]))
		offset += 3
		if offset+nameLength > listEnd {
			return "", fmt.Errorf("server name value truncated")
		}
		if nameType == 0 {
			name := strings.ToLower(strings.TrimSpace(string(data[offset : offset+nameLength])))
			return name, nil
		}
		offset += nameLength
	}
	return "", fmt.Errorf("server name list did not contain a host_name entry")
}
