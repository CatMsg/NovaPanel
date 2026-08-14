package service

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"math/big"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/util/common"
	mtls "github.com/metacubex/tls"
)

func (s *MasqueService) loadMasqueTLSCertificate(config *masqueInboundConfig) (mtls.Certificate, string, string, string, error) {
	if config != nil && strings.TrimSpace(config.PrivateKey) != "" {
		cert, err := generateMasqueTLSCertificate(config.PrivateKey)
		if err != nil {
			return mtls.Certificate{}, "", "", "", err
		}
		return cert, "", "", "inbound-key", nil
	}

	certFile, keyFile, err := s.resolveMasqueCertFiles(config.Host)
	if err != nil {
		return mtls.Certificate{}, "", "", "", err
	}
	cert, err := mtls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return mtls.Certificate{}, "", "", "", err
	}
	return cert, certFile, keyFile, "file", nil
}

func generateMasqueTLSCertificate(rawPrivateKey string) (mtls.Certificate, error) {
	priv, err := parseMasquePrivateKey(rawPrivateKey)
	if err != nil {
		return mtls.Certificate{}, err
	}

	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return mtls.Certificate{}, err
	}
	return mtls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
	}, nil
}

func parseMasquePrivateKey(raw string) (*ecdsa.PrivateKey, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, common.NewError("empty masque private key")
	}
	data, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}
	return x509.ParseECPrivateKey(data)
}

func (s *MasqueService) resolveMasqueCertFiles(host string) (string, string, error) {
	host = strings.TrimSpace(host)
	if host != "" {
		if certFile, keyFile, ok := resolveAcmeCertFiles(host); ok {
			return certFile, keyFile, nil
		}
	}

	allSetting, err := s.GetAllSetting()
	if err != nil {
		return "", "", err
	}

	candidates := [][2]string{
		{(*allSetting)["subCertFile"], (*allSetting)["subKeyFile"]},
		{(*allSetting)["webCertFile"], (*allSetting)["webKeyFile"]},
	}

	for _, candidate := range candidates {
		certFile := strings.TrimSpace(candidate[0])
		keyFile := strings.TrimSpace(candidate[1])
		if certFile == "" || keyFile == "" {
			continue
		}
		if err := fileMustExist(certFile); err != nil {
			continue
		}
		if err := fileMustExist(keyFile); err != nil {
			continue
		}
		if host != "" && !certificateMatchesDomain(certFile, host) {
			continue
		}
		return certFile, keyFile, nil
	}

	if host != "" {
		if webDomain := strings.TrimSpace((*allSetting)["webDomain"]); webDomain != "" && webDomain != host {
			if certFile, keyFile, ok := resolveAcmeCertFiles(webDomain); ok {
				return certFile, keyFile, nil
			}
		}
		if subDomain := strings.TrimSpace((*allSetting)["subDomain"]); subDomain != "" && subDomain != host {
			if certFile, keyFile, ok := resolveAcmeCertFiles(subDomain); ok {
				return certFile, keyFile, nil
			}
		}
	}

	return "", "", common.NewError("masque certificate and key are not configured")
}
