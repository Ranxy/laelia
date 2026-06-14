package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	errs "github.com/pkg/errors"
)

type TLSConfig struct {
	Domain  string
	Email   string
	CertDir string
	Hosts   []string
	DataDir string
}

func InitTLS(cfg *TLSConfig) (*tls.Config, error) {
	if cfg.Domain != "" {
		return initAutoCert(cfg)
	}

	certDir := cfg.CertDir
	if certDir == "" && cfg.DataDir != "" {
		certDir = filepath.Join(cfg.DataDir, "certs")
	}
	if certDir == "" {
		certDir = filepath.Join(".", "certs")
	}

	cert, err := loadOrGenerateCert(certDir, cfg.Hosts)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to load or generate TLS certificate")
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}, nil
}

func initAutoCert(_ *TLSConfig) (*tls.Config, error) {
	return nil, errs.New("auto-cert (ACME/Let's Encrypt) not yet implemented, use manual certs or self-signed mode")
}

func loadOrGenerateCert(certDir string, hosts []string) (tls.Certificate, error) {
	certFile := filepath.Join(certDir, "server.pem")
	keyFile := filepath.Join(certDir, "server.key")

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err == nil {
		return cert, nil
	}

	slog.Info("No TLS certificate found, generating self-signed CA and server certificate", "cert_dir", certDir)

	caCert, caKey, err := generateCA()
	if err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to generate CA")
	}

	serverCert, err := generateServerCert(certDir, caCert, caKey, hosts)
	if err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to generate server certificate")
	}

	caFingerprint := sha256Hex(caCert.Raw)
	slog.Info("CA fingerprint", "sha256", caFingerprint)
	slog.Info("Save this fingerprint for agent verification (or use --insecure)")

	return serverCert, nil
}

func generateCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, errs.Wrapf(err, "failed to generate CA key")
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, errs.Wrapf(err, "failed to generate CA serial")
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Laelia Self-Signed CA"},
			CommonName:   "Laelia CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, errs.Wrapf(err, "failed to create CA certificate")
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, errs.Wrapf(err, "failed to parse CA certificate")
	}

	return cert, key, nil
}

func generateServerCert(certDir string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, hosts []string) (tls.Certificate, error) {
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to generate server key")
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to generate server serial")
	}

	if len(hosts) == 0 {
		hosts = []string{"localhost", "127.0.0.1", "::1"}
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Laelia"},
			CommonName:   hosts[0],
		},
		NotBefore:   time.Now().Add(-1 * time.Hour),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to create server certificate")
	}

	if err := os.MkdirAll(certDir, 0700); err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to create cert directory")
	}

	keyBytes, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to marshal server key")
	}

	if err := os.WriteFile(filepath.Join(certDir, "server.key"), pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}), 0600); err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to write server key")
	}

	if err := os.WriteFile(filepath.Join(certDir, "server.pem"), pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	}), 0644); err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to write server certificate")
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCert.Raw,
	})
	if err := os.WriteFile(filepath.Join(certDir, "ca.pem"), caCertPEM, 0644); err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to write CA certificate")
	}

	slog.Info("Generated self-signed TLS certificates", "cert_dir", certDir, "hosts", hosts)

	cert, err := tls.LoadX509KeyPair(
		filepath.Join(certDir, "server.pem"),
		filepath.Join(certDir, "server.key"),
	)
	if err != nil {
		return tls.Certificate{}, errs.Wrapf(err, "failed to load generated key pair")
	}

	return cert, nil
}

func sha256Hex(data []byte) string {
	h := new(big.Int).SetBytes(data)
	return formatBigIntToHex(h)
}

func formatBigIntToHex(n *big.Int) string {
	return formatHex(n.Text(16))
}

func formatHex(s string) string {
	if len(s)%2 != 0 {
		s = "0" + s
	}
	return s
}

type ManagerVerifier struct {
	knownHostsPath string
	insecure       bool
}

func NewManagerVerifier(knownHostsPath string, insecure bool) *ManagerVerifier {
	return &ManagerVerifier{
		knownHostsPath: knownHostsPath,
		insecure:       insecure,
	}
}

func (v *ManagerVerifier) Verify(_ context.Context, _ string, rawCerts [][]byte) error {
	if v.insecure {
		return nil
	}

	if len(rawCerts) == 0 {
		return errs.New("no certificates provided by server")
	}

	fp := sha256Hex(rawCerts[0])

	saved, err := loadKnownHost(v.knownHostsPath)
	if err != nil || saved == "" {
		return errs.Errorf("manager fingerprint not known, run with --insecure or verify fingerprint: SHA256:%s", fp)
	}

	if saved != fp {
		return errs.Errorf("MANAGER FINGERPRINT CHANGED: expected SHA256:%s, got SHA256:%s (possible MITM)", saved, fp)
	}

	return nil
}

func loadKnownHost(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func SaveKnownHost(path string, fingerprint string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fingerprint), 0600)
}
