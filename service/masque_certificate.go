package service

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"os"
	"sync"
	"time"

	mtls "github.com/metacubex/tls"
)

const masqueCertificateCheckInterval = 2 * time.Second

type masqueFileStamp struct {
	modTime time.Time
	size    int64
}

type masqueCertificateSnapshot struct {
	CertFile     string
	KeyFile      string
	Source       string
	NotBefore    time.Time
	NotAfter     time.Time
	Fingerprint  string
	LastReloadAt time.Time
	ReloadCount  uint64
	LastError    string
}

type masqueCertificateReloader struct {
	mu            sync.Mutex
	current       *mtls.Certificate
	certFile      string
	keyFile       string
	source        string
	certStamp     masqueFileStamp
	keyStamp      masqueFileStamp
	lastCheckedAt time.Time
	snapshotValue masqueCertificateSnapshot
}

func newMasqueCertificateReloader(cert mtls.Certificate, certFile, keyFile, source string) *masqueCertificateReloader {
	reloader := &masqueCertificateReloader{
		current:  &cert,
		certFile: certFile,
		keyFile:  keyFile,
		source:   source,
	}
	reloader.certStamp, _ = masqueFileInfo(certFile)
	reloader.keyStamp, _ = masqueFileInfo(keyFile)
	reloader.snapshotValue = certificateSnapshot(cert, certFile, keyFile, source)
	reloader.snapshotValue.LastReloadAt = time.Now()
	return reloader
}

func (r *masqueCertificateReloader) getCertificate(_ *mtls.ClientHelloInfo) (*mtls.Certificate, error) {
	r.reloadIfChanged(false)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current, nil
}

func (r *masqueCertificateReloader) snapshot() masqueCertificateSnapshot {
	if r == nil {
		return masqueCertificateSnapshot{}
	}
	r.reloadIfChanged(false)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotValue
}

func (r *masqueCertificateReloader) reloadIfChanged(force bool) {
	if r == nil || r.certFile == "" || r.keyFile == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if !force && now.Sub(r.lastCheckedAt) < masqueCertificateCheckInterval {
		return
	}
	r.lastCheckedAt = now

	certStamp, certErr := masqueFileInfo(r.certFile)
	keyStamp, keyErr := masqueFileInfo(r.keyFile)
	if certErr != nil {
		r.snapshotValue.LastError = certErr.Error()
		return
	}
	if keyErr != nil {
		r.snapshotValue.LastError = keyErr.Error()
		return
	}
	if certStamp == r.certStamp && keyStamp == r.keyStamp {
		return
	}

	cert, err := mtls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		r.snapshotValue.LastError = err.Error()
		return
	}
	next := certificateSnapshot(cert, r.certFile, r.keyFile, r.source)
	next.LastReloadAt = now
	next.ReloadCount = r.snapshotValue.ReloadCount + 1
	r.current = &cert
	r.certStamp = certStamp
	r.keyStamp = keyStamp
	r.snapshotValue = next
}

func certificateSnapshot(cert mtls.Certificate, certFile, keyFile, source string) masqueCertificateSnapshot {
	result := masqueCertificateSnapshot{
		CertFile: certFile,
		KeyFile:  keyFile,
		Source:   source,
	}
	if len(cert.Certificate) == 0 {
		result.LastError = "certificate chain is empty"
		return result
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		result.LastError = err.Error()
		return result
	}
	sum := sha256.Sum256(leaf.Raw)
	result.NotBefore = leaf.NotBefore
	result.NotAfter = leaf.NotAfter
	result.Fingerprint = hex.EncodeToString(sum[:])
	return result
}

func (s masqueCertificateSnapshot) statusFields() map[string]interface{} {
	result := map[string]interface{}{}
	if s.CertFile != "" {
		result["cert_file"] = s.CertFile
	}
	if s.KeyFile != "" {
		result["key_file"] = s.KeyFile
	}
	if s.Source != "" {
		result["cert_source"] = s.Source
	}
	if !s.NotBefore.IsZero() {
		result["cert_not_before"] = s.NotBefore.Format(time.RFC3339)
	}
	if !s.NotAfter.IsZero() {
		result["cert_not_after"] = s.NotAfter.Format(time.RFC3339)
		result["cert_days_remaining"] = int(time.Until(s.NotAfter).Hours() / 24)
	}
	if s.Fingerprint != "" {
		result["cert_fingerprint"] = s.Fingerprint
	}
	if !s.LastReloadAt.IsZero() {
		result["cert_last_reload_at"] = s.LastReloadAt.Format(time.RFC3339)
	}
	result["cert_reload_count"] = s.ReloadCount
	if s.LastError != "" {
		result["cert_reload_error"] = s.LastError
	}
	return result
}

func masqueFileInfo(path string) (masqueFileStamp, error) {
	if path == "" {
		return masqueFileStamp{}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return masqueFileStamp{}, err
	}
	return masqueFileStamp{modTime: info.ModTime(), size: info.Size()}, nil
}
