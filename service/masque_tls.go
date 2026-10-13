package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
)

func normalizeMasqueCertificateHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return address.String(), nil
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" || strings.ContainsAny(host, ":/\\ \t\r\n") {
		return "", errors.New("MASQUE server must be a hostname or IP address")
	}
	return host, nil
}

// ensureMasqueTLSMaterial creates an inbound-specific ECDSA identity and
// renews only its certificate when the configured server name changes.
func ensureMasqueTLSMaterial(inbound *model.Inbound, host string) (bool, error) {
	if inbound == nil {
		return false, errors.New("missing MASQUE inbound")
	}
	name, err := normalizeMasqueCertificateHost(host)
	if err != nil {
		return false, err
	}
	var privateKey *ecdsa.PrivateKey
	createdKey := false
	switch {
	case inbound.MasqueTLSKey == "" && inbound.MasqueTLSCertificate == "":
		privateKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return false, err
		}
		createdKey = true
	case inbound.MasqueTLSKey == "" || inbound.MasqueTLSCertificate == "":
		return false, errors.New("incomplete stored MASQUE TLS identity")
	default:
		block, _ := pem.Decode([]byte(inbound.MasqueTLSKey))
		if block == nil || block.Type != "EC PRIVATE KEY" {
			return false, errors.New("invalid stored MASQUE ECDSA private key")
		}
		privateKey, err = x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return false, errors.New("invalid stored MASQUE ECDSA private key")
		}
		certificate, err := parseMasqueCertificate(inbound.MasqueTLSCertificate)
		if err != nil {
			return false, err
		}
		publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
		if !ok || !publicKey.Equal(&privateKey.PublicKey) {
			return false, errors.New("stored MASQUE certificate and private key do not match")
		}
		if certificate.VerifyHostname(name) == nil && time.Until(certificate.NotAfter) > 30*24*time.Hour {
			return false, nil
		}
	}
	certificatePEM, err := issueMasqueCertificate(privateKey, name)
	if err != nil {
		return false, err
	}
	if createdKey {
		keyDER, err := x509.MarshalECPrivateKey(privateKey)
		if err != nil {
			return false, err
		}
		inbound.MasqueTLSKey = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	}
	inbound.MasqueTLSCertificate = certificatePEM
	return true, nil
}

func issueMasqueCertificate(privateKey *ecdsa.PrivateKey, host string) (string, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if address, err := netip.ParseAddr(host); err == nil {
		template.IPAddresses = []net.IP{net.IP(address.AsSlice())}
	} else {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", fmt.Errorf("create MASQUE ECDSA certificate: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
}

func parseMasqueCertificate(value string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid stored MASQUE TLS certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid stored MASQUE TLS certificate")
	}
	if _, ok := certificate.PublicKey.(*ecdsa.PublicKey); !ok {
		return nil, errors.New("stored MASQUE TLS certificate is not ECDSA")
	}
	return certificate, nil
}

func validateMasqueTLSMaterial(inbound *model.Inbound, host string) (*x509.Certificate, error) {
	if inbound == nil || inbound.MasqueTLSCertificate == "" || inbound.MasqueTLSKey == "" {
		return nil, errors.New("MASQUE ECDSA identity is not initialized; save the inbound again")
	}
	name, err := normalizeMasqueCertificateHost(host)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode([]byte(inbound.MasqueTLSKey))
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, errors.New("invalid stored MASQUE ECDSA private key")
	}
	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid stored MASQUE ECDSA private key")
	}
	certificate, err := parseMasqueCertificate(inbound.MasqueTLSCertificate)
	if err != nil {
		return nil, err
	}
	publicKey, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || !publicKey.Equal(&privateKey.PublicKey) {
		return nil, errors.New("stored MASQUE certificate and private key do not match")
	}
	if err := certificate.VerifyHostname(name); err != nil {
		return nil, errors.New("stored MASQUE certificate does not match the inbound server name")
	}
	if time.Until(certificate.NotAfter) <= 0 {
		return nil, errors.New("stored MASQUE certificate has expired")
	}
	return certificate, nil
}

func masqueServerTLSOptions(inbound *model.Inbound, host string) (map[string]any, error) {
	if _, err := validateMasqueTLSMaterial(inbound, host); err != nil {
		return nil, err
	}
	name, err := normalizeMasqueCertificateHost(host)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"enabled":     true,
		"server_name": name,
		"alpn":        []string{"h3"},
		"min_version": "1.3",
		"max_version": "1.3",
		"certificate": []string{inbound.MasqueTLSCertificate},
		"key":         []string{inbound.MasqueTLSKey},
	}, nil
}
