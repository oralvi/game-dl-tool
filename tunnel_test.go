package main

import (
	"crypto/tls"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseServerNameFromTLSRecord(t *testing.T) {
	serverName := "unit-test-route.example"
	clientSide, serverSide := net.Pipe()
	defer clientSide.Close()
	defer serverSide.Close()

	go func() {
		client := tls.Client(clientSide, &tls.Config{
			ServerName:         serverName,
			InsecureSkipVerify: true,
		})
		_ = client.SetDeadline(time.Now().Add(2 * time.Second))
		_ = client.Handshake()
	}()

	record, hostname, err := readTLSClientHello(serverSide)
	if err != nil {
		t.Fatalf("readTLSClientHello() error = %v", err)
	}
	if len(record) == 0 {
		t.Fatalf("expected non-empty client hello record")
	}
	if hostname != serverName {
		t.Fatalf("hostname = %q, want %q", hostname, serverName)
	}
}

func TestLocalTunnelManagerSetRoutes(t *testing.T) {
	manager := newLocalTunnelManager(nil)
	domain := "download-node.example"
	upstreamIP := net.ParseIP("2001:db8::42")
	if upstreamIP == nil {
		t.Fatalf("expected test upstream IP to parse")
	}
	rows := []resultRow{
		{
			Domain:  domain,
			Address: upstreamIP.String(),
			Family:  family6,
		},
	}
	aliases := map[string][]string{
		domain: {
			domain,
			"edge-a.example",
			"edge-b.example",
		},
	}

	count := manager.SetRoutes(rows, aliases)
	if count != len(aliases[domain]) {
		t.Fatalf("route count = %d, want %d", count, len(aliases[domain]))
	}

	for _, hostname := range aliases[domain] {
		route, ok := manager.lookupRoute(hostname)
		if !ok {
			t.Fatalf("missing route for %s", hostname)
		}
		if !strings.EqualFold(route.UpstreamIP, upstreamIP.String()) {
			t.Fatalf("route.UpstreamIP = %q, want %q", route.UpstreamIP, upstreamIP.String())
		}
	}
}
