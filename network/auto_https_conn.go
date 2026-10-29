package network

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type AutoHttpsConn struct {
	net.Conn

	firstBuf     []byte
	bufStart     int
	allowedHosts []string

	readRequestOnce sync.Once
}

func NewAutoHttpsConn(conn net.Conn, allowedHosts ...string) net.Conn {
	return &AutoHttpsConn{
		Conn:         conn,
		allowedHosts: append([]string(nil), allowedHosts...),
	}
}

func TLSCertificateHosts(certificate tls.Certificate) []string {
	leaf := certificate.Leaf
	if leaf == nil && len(certificate.Certificate) > 0 {
		leaf, _ = x509.ParseCertificate(certificate.Certificate[0])
	}
	if leaf == nil {
		return nil
	}

	hosts := append([]string(nil), leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		hosts = append(hosts, ip.String())
	}
	if len(hosts) == 0 && leaf.Subject.CommonName != "" {
		hosts = append(hosts, leaf.Subject.CommonName)
	}
	return hosts
}

func (c *AutoHttpsConn) readRequest() bool {
	c.firstBuf = make([]byte, 0, 2048)
	var chunk [2048]byte
	requestLineValid := false
	for {
		readSize := len(chunk)
		if remaining := maxHTTPHeaderBytes - len(c.firstBuf); remaining < readSize {
			readSize = remaining
		}
		if readSize == 0 {
			if requestLineValid {
				return c.writeHTTPResponse(nil, http.StatusRequestHeaderFieldsTooLarge, "")
			}
			return false
		}

		n, readErr := c.Conn.Read(chunk[:readSize])
		if n > 0 {
			c.firstBuf = append(c.firstBuf, chunk[:n]...)
		}
		headerEnd := bytes.Index(c.firstBuf, []byte("\r\n\r\n"))
		headerData := c.firstBuf
		if headerEnd >= 0 {
			headerData = c.firstBuf[:headerEnd+4]
		}
		if len(headerData) > 0 && !possibleHTTPHeader(headerData) {
			return false
		}

		lineEnd := bytes.Index(c.firstBuf, []byte("\r\n"))
		if lineEnd >= 0 && !requestLineValid {
			if !validHTTP1RequestLine(c.firstBuf[:lineEnd]) {
				return false
			}
			requestLineValid = true
		}
		if requestLineValid && bytes.Contains(c.firstBuf, []byte("\r\n\r\n")) {
			break
		}
		if readErr != nil {
			if requestLineValid {
				return c.writeHTTPResponse(nil, http.StatusBadRequest, "")
			}
			return false
		}
		if len(c.firstBuf) == maxHTTPHeaderBytes {
			if requestLineValid {
				return c.writeHTTPResponse(nil, http.StatusRequestHeaderFieldsTooLarge, "")
			}
			return false
		}
	}

	request, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(c.firstBuf)))
	if err != nil {
		return c.writeHTTPResponse(nil, http.StatusBadRequest, "")
	}
	if host, port, ok := splitHTTPAuthority(request.Host); ok && matchesAllowedHTTPSHost(host, c.allowedHosts) {
		authority := host
		if strings.Contains(host, ":") {
			authority = "[" + host + "]"
		}
		if port != "" {
			authority = net.JoinHostPort(host, port)
		}
		requestURI := request.URL.RequestURI()
		if requestURI == "" {
			requestURI = "/"
		}
		return c.writeHTTPResponse(request, http.StatusTemporaryRedirect, fmt.Sprintf("https://%s%s", authority, requestURI))
	}
	return c.writeHTTPResponse(request, http.StatusMisdirectedRequest, "")
}

func (c *AutoHttpsConn) writeHTTPResponse(request *http.Request, status int, location string) bool {
	major, minor := 1, 1
	if request != nil {
		major, minor = request.ProtoMajor, request.ProtoMinor
	}
	response := http.Response{
		Header:     http.Header{"Cache-Control": []string{"no-store"}},
		StatusCode: status,
		ProtoMajor: major,
		ProtoMinor: minor,
		Request:    request,
		Body:       http.NoBody,
		Close:      true,
	}
	if location != "" {
		response.Header.Set("Location", location)
	}
	writeErr := response.Write(c.Conn)
	_ = c.Close()
	c.firstBuf = nil
	return writeErr == nil
}

func possibleHTTPHeader(data []byte) bool {
	if len(data) == 0 || data[0] < 0x21 || data[0] > 0x7e {
		return false
	}
	for _, value := range data {
		if value == 0x7f || value < 0x20 && value != '\r' && value != '\n' && value != '\t' {
			return false
		}
	}
	return true
}

func validHTTP1RequestLine(line []byte) bool {
	parts := strings.Split(string(line), " ")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || (parts[2] != "HTTP/1.0" && parts[2] != "HTTP/1.1") {
		return false
	}
	for _, value := range []byte(parts[0]) {
		if !isHTTPTokenByte(value) {
			return false
		}
	}
	for _, value := range []byte(parts[1]) {
		if value <= 0x20 || value == 0x7f {
			return false
		}
	}
	return true
}

func isHTTPTokenByte(value byte) bool {
	if value >= '0' && value <= '9' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' {
		return true
	}
	switch value {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}

func splitHTTPAuthority(authority string) (host, port string, ok bool) {
	if authority == "" {
		return "", "", false
	}
	if strings.HasPrefix(authority, "[") {
		if host, port, err := net.SplitHostPort(authority); err == nil {
			if !validHTTPPort(port) {
				return "", "", false
			}
			return normalizeHTTPSHost(host), port, true
		}
		if strings.HasSuffix(authority, "]") {
			return normalizeHTTPSHost(strings.TrimSuffix(strings.TrimPrefix(authority, "["), "]")), "", true
		}
		return "", "", false
	}
	if strings.Count(authority, ":") > 1 {
		return "", "", false
	}
	if strings.Contains(authority, ":") {
		host, port, err := net.SplitHostPort(authority)
		if err != nil || port == "" {
			return "", "", false
		}
		if !validHTTPPort(port) {
			return "", "", false
		}
		return normalizeHTTPSHost(host), port, true
	}
	return normalizeHTTPSHost(authority), "", true
}

func validHTTPPort(port string) bool {
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	portNumber, err := strconv.Atoi(port)
	return err == nil && portNumber >= 1 && portNumber <= 65535
}

func normalizeHTTPSHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func matchesAllowedHTTPSHost(host string, allowedHosts []string) bool {
	for _, allowed := range allowedHosts {
		pattern := normalizeHTTPSHost(allowed)
		if pattern == host {
			return true
		}
		if strings.HasPrefix(pattern, "*.") {
			suffix := pattern[1:]
			prefix := strings.TrimSuffix(host, suffix)
			if prefix != "" && !strings.Contains(prefix, ".") && strings.HasSuffix(host, suffix) {
				return true
			}
		}
	}
	return false
}

func (c *AutoHttpsConn) Read(buf []byte) (int, error) {
	c.readRequestOnce.Do(func() {
		c.readRequest()
	})

	if c.firstBuf != nil {
		n := copy(buf, c.firstBuf[c.bufStart:])
		c.bufStart += n
		if c.bufStart >= len(c.firstBuf) {
			c.firstBuf = nil
		}
		return n, nil
	}

	return c.Conn.Read(buf)
}
