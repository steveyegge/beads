package ui

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"time"
)

// GenerateSelfSignedCertificate returns a self-signed X.509 certificate and private key in
// PEM encoding. The provided hosts are added to the certificate's Subject Alternative Name
// extension. When hosts is empty, localhost and loopback IPs are used by default.
func GenerateSelfSignedCertificate(hosts []string, validFor time.Duration) ([]byte, []byte, error) {
	if validFor <= 0 {
		validFor = 365 * 24 * time.Hour
	}

	seen := make(map[string]struct{})
	var dnsNames []string
	var ipAddrs []net.IP

	defaultHosts := []string{"127.0.0.1", "::1", "localhost"}
	if len(hosts) == 0 {
		hosts = defaultHosts
	}

	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		if ip := net.ParseIP(host); ip != nil {
			ipAddrs = append(ipAddrs, ip)
			continue
		}
		dnsNames = append(dnsNames, host)
	}

	for _, host := range defaultHosts {
		if _, exists := seen[host]; exists {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			ipAddrs = append(ipAddrs, ip)
			continue
		}
		dnsNames = append(dnsNames, host)
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "beads-ui",
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddrs,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return certPEM, keyPEM, nil
}
