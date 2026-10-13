package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const remoteBackupMaxSize int64 = 1 << 30

// No custom CA, insecure TLS switch, shell command, or redirect is accepted.
// The HTTP client is replaceable only by package tests, not by configuration.
type backupDAV struct {
	base               *url.URL
	username, password string
	client             *http.Client
}

func backupDestination(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("destination must be HTTPS without URL credentials, query, or fragment")
	}
	if strings.ContainsAny(u.Path, "\\\x00\r\n") || u.RawPath != "" {
		return nil, errors.New("destination path must not contain encoded or unsafe separators")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return nil, errors.New("destination path must not contain dot segments")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	return u, nil
}

func newBackupDAV(cfg remoteBackupConfig, owner string) (*backupDAV, error) {
	u, err := backupDestination(cfg.Destination)
	if err != nil {
		return nil, err
	}
	if !backupOwnerPattern.MatchString(owner) {
		return nil, errors.New("invalid backup owner")
	}
	u.Path += "novapanel-" + owner + "/"
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		DisableCompression:    true,
	}
	return &backupDAV{base: u, username: cfg.Username, password: cfg.Password, client: &http.Client{
		Transport: transport, Timeout: 90 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") },
	}}, nil
}

func (d *backupDAV) close() { d.client.CloseIdleConnections() }

func (d *backupDAV) objectURL(name string) string {
	u := *d.base
	u.Path += name
	return u.String()
}

func (d *backupDAV) request(ctx context.Context, method, name string, body io.Reader, headers map[string]string, statuses ...int) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.objectURL(name), body)
	if err != nil {
		return nil, errors.New("cannot construct WebDAV request")
	}
	req.SetBasicAuth(d.username, d.password)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		// net/http errors include URLs and sometimes proxy details. Never relay them.
		if ctx.Err() != nil {
			return nil, errors.New("backup operation cancelled or timed out")
		}
		return nil, errors.New("WebDAV transport failed (TLS, connection, redirect, or timeout)")
	}
	for _, status := range statuses {
		if resp.StatusCode == status {
			return resp, nil
		}
	}
	_ = resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errors.New("WebDAV authentication or permission denied")
	}
	return nil, fmt.Errorf("WebDAV %s failed (HTTP %d)", method, resp.StatusCode)
}

func (d *backupDAV) ensure(ctx context.Context) error {
	resp, err := d.request(ctx, "MKCOL", "", nil, nil, 201, 405)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	_, err = d.list(ctx)
	return err
}

func (d *backupDAV) put(ctx context.Context, name string, body io.Reader) error {
	resp, err := d.request(ctx, "PUT", name, body, map[string]string{"If-None-Match": "*", "Content-Type": "application/octet-stream"}, 201, 204)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

func (d *backupDAV) move(ctx context.Context, from, to string) error {
	resp, err := d.request(ctx, "MOVE", from, nil, map[string]string{"Destination": d.objectURL(to), "Overwrite": "F"}, 201)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

func (d *backupDAV) delete(ctx context.Context, name string) error {
	// Only exact generated file names, never collections or user paths.
	if !backupArtifactPattern.MatchString(name) {
		return errors.New("refusing to delete non-backup artifact")
	}
	resp, err := d.request(ctx, "DELETE", name, nil, nil, 200, 204, 404)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

// Only files constructed by this operation may reach this cleanup helper.
func (d *backupDAV) cleanupStaging(name string) {
	if !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".upload") || !backupArtifactPattern.MatchString(strings.TrimSuffix(strings.TrimPrefix(name, "."), ".upload")) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := d.request(ctx, "DELETE", name, nil, nil, 200, 204, 404)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func (d *backupDAV) read(ctx context.Context, name string, limit int64, out io.Writer) error {
	resp, err := d.request(ctx, "GET", name, nil, nil, 200)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > limit {
		return errors.New("remote artifact exceeds size limit")
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return errors.New("remote artifact download failed or timed out")
	}
	if n > limit {
		return errors.New("remote artifact exceeds size limit")
	}
	return nil
}

func (d *backupDAV) list(ctx context.Context) ([]string, error) {
	body := strings.NewReader(`<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`)
	resp, err := d.request(ctx, "PROPFIND", "", body, map[string]string{"Depth": "1", "Content-Type": "application/xml"}, 207)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || n > 2<<20 {
		return nil, errors.New("WebDAV listing too large or unreadable")
	}
	var listing struct {
		XMLName   xml.Name `xml:"DAV: multistatus"`
		Responses []struct {
			Href string `xml:"DAV: href"`
		} `xml:"DAV: response"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &listing); err != nil {
		return nil, errors.New("invalid WebDAV directory listing")
	}
	if len(listing.Responses) > 10000 {
		return nil, errors.New("too many remote backup entries")
	}
	seen := map[string]bool{}
	var names []string
	for _, response := range listing.Responses {
		u, err := url.Parse(response.Href)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			continue
		}
		if u.IsAbs() && (u.Scheme != d.base.Scheme || u.Host != d.base.Host) {
			continue
		}
		if u.Host != "" && u.Host != d.base.Host {
			continue
		}
		// Compare decoded absolute paths; only direct children of our namespace.
		if !strings.HasPrefix(u.Path, d.base.Path) {
			continue
		}
		name := strings.TrimPrefix(u.Path, d.base.Path)
		if !backupArtifactPattern.MatchString(name) || !strings.HasSuffix(name, ".json") || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}
