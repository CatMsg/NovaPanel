package network

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAutoHTTPSRedirectRequiresAllowedHost(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		allowedHost []string
		chunks      []string
		status      int
		location    string
	}{
		{
			name:        "allowed host preserves port and request URI",
			host:        "panel.example:9999",
			allowedHost: []string{"panel.example"},
			chunks:      []string{"GET /login?next=%2F HTTP/1.1\r\nHost: panel.", "example:9999\r\n\r\n"},
			status:      http.StatusTemporaryRedirect,
			location:    "https://panel.example:9999/login?next=%2F",
		},
		{
			name:        "binary body does not look like a TLS handshake",
			allowedHost: []string{"panel.example"},
			chunks:      []string{"POST /app/api/importdb HTTP/1.1\r\nHost: panel.example\r\nContent-Length: 4\r\n\r\n\x00\xff\x00\xff"},
			status:      http.StatusTemporaryRedirect,
			location:    "https://panel.example/app/api/importdb",
		},
		{
			name:        "foreign host is not reflected into redirect",
			host:        "attacker.example:9999",
			allowedHost: []string{"panel.example"},
			status:      http.StatusMisdirectedRequest,
		},
		{
			name:   "missing allowlist fails closed",
			host:   "panel.example",
			status: http.StatusMisdirectedRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, server := net.Pipe()
			deadline := time.Now().Add(2 * time.Second)
			_ = client.SetDeadline(deadline)
			_ = server.SetDeadline(deadline)
			wrapped := NewAutoHttpsConn(server, tt.allowedHost...)
			readDone := make(chan struct{})
			go func() {
				_, _ = wrapped.Read(make([]byte, 4096))
				close(readDone)
			}()

			chunks := tt.chunks
			if len(chunks) == 0 {
				chunks = []string{"GET /login?next=%2F HTTP/1.1\r\nHost: " + tt.host + "\r\n\r\n"}
			}
			for _, chunk := range chunks {
				if _, err := io.WriteString(client, chunk); err != nil {
					t.Fatalf("write request: %v", err)
				}
			}
			response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: http.MethodGet})
			if err != nil {
				t.Fatalf("read redirect response: %v", err)
			}
			_ = response.Body.Close()
			_ = client.Close()
			select {
			case <-readDone:
			case <-time.After(2 * time.Second):
				t.Fatal("connection read did not finish")
			}

			if response.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, tt.status)
			}
			if location := response.Header.Get("Location"); location != tt.location {
				t.Fatalf("Location = %q, want %q", location, tt.location)
			}
		})
	}
}

func TestTLSCertificateHostsReadsSANs(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	hosts := TLSCertificateHosts(server.TLS.Certificates[0])
	if len(hosts) == 0 {
		t.Fatal("certificate SANs were not returned")
	}
	for _, host := range hosts {
		if strings.TrimSpace(host) != host {
			t.Fatalf("certificate host contains surrounding whitespace: %q", host)
		}
	}
}

func TestAutoHTTPSConnectionPreservesTLSHandshakeBytes(t *testing.T) {
	client, server := net.Pipe()
	deadline := time.Now().Add(2 * time.Second)
	_ = client.SetDeadline(deadline)
	_ = server.SetDeadline(deadline)
	wrapped := NewAutoHttpsConn(server, "panel.example")
	want := []byte{0x16, 0x03, 0x01, 0x00, 0x01, 0x00}
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write(want)
		writeDone <- err
	}()

	got := make([]byte, len(want))
	n, err := wrapped.Read(got)
	if err != nil {
		t.Fatalf("read TLS preface: %v", err)
	}
	if n != len(want) || string(got[:n]) != string(want) {
		t.Fatalf("TLS preface = %v, want %v", got[:n], want)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write TLS preface: %v", err)
	}
	_ = client.Close()
}

func TestAutoHTTPSConnectionBoundsPlainHTTPHeaders(t *testing.T) {
	client, server := net.Pipe()
	deadline := time.Now().Add(3 * time.Second)
	_ = client.SetDeadline(deadline)
	_ = server.SetDeadline(deadline)
	wrapped := NewAutoHttpsConn(server, "panel.example")
	go func() {
		_, _ = wrapped.Read(make([]byte, 4096))
	}()

	request := "GET / HTTP/1.1\r\nHost: panel.example\r\nX-Large: " + strings.Repeat("a", maxHTTPHeaderBytes) + "\r\n\r\n"
	writeDone := make(chan error, 1)
	go func() {
		_, err := io.WriteString(client, request)
		writeDone <- err
	}()
	response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("read oversized-header response: %v", err)
	}
	_ = response.Body.Close()
	_ = client.Close()
	if response.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusRequestHeaderFieldsTooLarge)
	}
	select {
	case <-writeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("oversized request writer did not finish")
	}
}
