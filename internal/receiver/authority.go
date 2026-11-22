package receiver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/0x3639/bridge-health/internal/localstate"
)

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func certificate(name string, expires time.Time) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	return &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().UTC().Add(-5 * time.Minute), NotAfter: expires.UTC(),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true}, nil
}

func openAuthority(dir string) (*authority, error) {
	path := filepath.Join(dir, "authority.json")
	type saved struct{ Certificate, Key []byte }
	var s saved
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// An existing fleet must never silently switch its trust root.
		if _, err := os.Stat(filepath.Join(dir, "fleet.json")); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("restore the missing receiver authority from backup")
		}
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		root, err := certificate("Bridge Health telemetry CA", time.Now().AddDate(10, 0, 0))
		if err != nil {
			return nil, err
		}
		root.IsCA, root.MaxPathLenZero = true, true
		root.KeyUsage = x509.KeyUsageCertSign
		s.Certificate, err = x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
		if err != nil {
			return nil, err
		}
		s.Key, err = x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		if err = localstate.Write(path, s); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &s); err != nil {
			return nil, err
		}
	}
	cert, err := x509.ParseCertificate(s.Certificate)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParseECPrivateKey(s.Key)
	if err != nil {
		return nil, err
	}
	if !key.PublicKey.Equal(cert.PublicKey) || cert.CheckSignatureFrom(cert) != nil || !time.Now().Before(cert.NotAfter) {
		return nil, errors.New("invalid or expired receiver authority")
	}
	return &authority{cert, key}, nil
}

func certPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func (a *authority) issueClient(csr *x509.CertificateRequest, id string) (*x509.Certificate, error) {
	expiry := time.Now().AddDate(1, 0, 0)
	if a.cert.NotAfter.Before(expiry) {
		expiry = a.cert.NotAfter
	}
	c, err := certificate(id, expiry)
	if err != nil {
		return nil, err
	}
	c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	der, err := x509.CreateCertificate(rand.Reader, c, a.cert, csr.PublicKey, a.key)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}

func (a *authority) serverTLS(host string) (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	c, err := certificate(host, a.cert.NotAfter)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		c.IPAddresses = []net.IP{ip}
	} else {
		c.DNSNames = []string{host}
	}
	c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	der, err := x509.CreateCertificate(rand.Reader, c, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, fmt.Errorf("create receiver TLS certificate: %w", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(a.cert)
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}, nil
}
