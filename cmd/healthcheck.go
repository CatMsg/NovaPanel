package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/config"
)

// checkLocalPanel probes the configured local listener, never the public DNS
// address. TLS remains verified against the certificate installed on this host.
func checkLocalPanel() error {
	path, err := filepath.Abs(config.GetDBPath())
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro&_busy_timeout=3000"}).String())
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.Query("SELECT key, value FROM settings WHERE key IN ('webPort', 'webListen', 'webPath', 'webDomain', 'webCertFile', 'webKeyFile')")
	if err != nil {
		return err
	}
	settings := map[string]string{"webPort": "2095", "webPath": "/app/"}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			return err
		}
		settings[key] = value
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	return probeLocalPanel(settings)
}

func probeLocalPanel(settings map[string]string) error {
	host := strings.Trim(settings["webListen"], "[]")
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	if net.ParseIP(host) == nil {
		return fmt.Errorf("healthcheck requires a local IP listener, got %q", host)
	}
	scheme := "http"
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}
	defer transport.CloseIdleConnections()
	if settings["webCertFile"] != "" || settings["webKeyFile"] != "" {
		pair, err := tls.LoadX509KeyPair(settings["webCertFile"], settings["webKeyFile"])
		if err != nil {
			return err
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return err
		}
		roots := x509.NewCertPool()
		for _, der := range pair.Certificate {
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				return err
			}
			roots.AddCert(cert)
		}
		serverName := settings["webDomain"]
		if serverName == "" && len(leaf.DNSNames) > 0 {
			serverName = strings.Replace(leaf.DNSNames[0], "*", "healthcheck", 1)
		}
		if serverName == "" {
			serverName = host
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: serverName}
		scheme = "https"
	}
	path := "/" + strings.TrimPrefix(settings["webPath"], "/")
	request, err := http.NewRequest(http.MethodGet, (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, settings["webPort"]), Path: path}).String(), nil)
	if err != nil {
		return err
	}
	if domain := settings["webDomain"]; domain != "" {
		request.Host = domain
	}
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return fmt.Errorf("local panel returned HTTP %d", response.StatusCode)
	}
	return nil
}
