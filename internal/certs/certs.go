// Package certs manages Tapis' local TLS identity: a private certificate
// authority that is name-constrained to private network addresses, and a
// server certificate signed by it. Nothing here ever talks to the Internet.
package certs

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Identity holds the loaded CA and server certificate.
type Identity struct {
	CACert   *x509.Certificate
	CAPEM    []byte
	TLS      tls.Certificate
	LeafCert *x509.Certificate
}

// Fingerprint returns the SHA-256 fingerprint of the server certificate,
// formatted like browsers display it.
func (id *Identity) Fingerprint() string { return fingerprint(id.LeafCert.Raw) }

// CAFingerprint returns the SHA-256 fingerprint of the CA certificate.
func (id *Identity) CAFingerprint() string { return fingerprint(id.CACert.Raw) }

func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// Address ranges the CA may ever certify. Even if the CA key leaked, it
// could not be used to impersonate a public website.
var permittedRanges = []*net.IPNet{
	mustCIDR("10.0.0.0/8"),
	mustCIDR("172.16.0.0/12"),
	mustCIDR("192.168.0.0/16"),
	mustCIDR("169.254.0.0/16"),
	mustCIDR("127.0.0.0/8"),
	mustCIDR("fc00::/7"),
	mustCIDR("fe80::/10"),
	mustCIDR("::1/128"),
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		panic(err)
	}
	return n
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	var buf bytes.Buffer
	pem.Encode(&buf, &pem.Block{Type: typ, Bytes: der})
	return os.WriteFile(path, buf.Bytes(), mode)
}

func readPEM(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, errors.New("invalid PEM in " + path)
	}
	return blk.Bytes, nil
}

func loadCA(dir string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certDER, err := readPEM(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := readPEM(filepath.Join(dir, "ca.key"))
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParseECPrivateKey(keyDER)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func createCA(dir, host string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber:                serial(),
		Subject:                     pkix.Name{CommonName: "Tapis local CA (" + host + ")", Organization: []string{"Tapis"}},
		NotBefore:                   time.Now().Add(-time.Hour),
		NotAfter:                    time.Now().AddDate(10, 0, 0),
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid:       true,
		IsCA:                        true,
		MaxPathLenZero:              true,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{"local", "localhost"},
		PermittedIPRanges:           permittedRanges,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	if err := writePEM(filepath.Join(dir, "ca.key"), "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return nil, nil, err
	}
	if err := writePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", der, 0o644); err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	return cert, key, err
}

func leafCovers(cert *x509.Certificate, ips []net.IP) bool {
	if time.Now().Add(30 * 24 * time.Hour).After(cert.NotAfter) {
		return false
	}
	for _, ip := range ips {
		found := false
		for _, c := range cert.IPAddresses {
			if c.Equal(ip) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func dnsLabel(host string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(host) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

// Load loads (or creates) the CA and a server certificate that covers ips.
// The server certificate is only regenerated when the set of addresses
// changes, so phones that accepted it keep trusting it.
func Load(dir string, ips []net.IP) (*Identity, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	label := dnsLabel(host)
	if label == "" {
		label = "tapis"
	}
	caCert, caKey, err := loadCA(dir)
	if err != nil {
		if caCert, caKey, err = createCA(dir, host); err != nil {
			return nil, fmt.Errorf("creating CA: %w", err)
		}
	}

	leafPath, keyPath := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
	if der, err := readPEM(leafPath); err == nil {
		if leaf, err := x509.ParseCertificate(der); err == nil && leafCovers(leaf, ips) &&
			leaf.CheckSignatureFrom(caCert) == nil {
			if tc, err := tls.LoadX509KeyPair(leafPath, keyPath); err == nil {
				return &Identity{CACert: caCert, CAPEM: pemOf(caCert.Raw), TLS: tc, LeafCert: leaf}, nil
			}
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	all := append([]net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback}, ips...)
	tpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: label + ".local", Organization: []string{"Tapis"}},
		NotBefore:    time.Now().Add(-time.Hour),
		// Browsers reject leaf certificates valid for more than ~398 days.
		NotAfter:    time.Now().AddDate(1, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{label + ".local", "localhost"},
		IPAddresses: all,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("creating server certificate: %w", err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return nil, err
	}
	if err := writePEM(leafPath, "CERTIFICATE", der, 0o644); err != nil {
		return nil, err
	}
	tc, err := tls.LoadX509KeyPair(leafPath, keyPath)
	if err != nil {
		return nil, err
	}
	leaf, _ := x509.ParseCertificate(der)
	return &Identity{CACert: caCert, CAPEM: pemOf(caCert.Raw), TLS: tc, LeafCert: leaf}, nil
}

func pemOf(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
