package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	path string
}
type credential struct{ cert, key, fp string }

func makeKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func template(t *testing.T, name string) *x509.Certificate {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	return &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
}
func savePEM(t *testing.T, path, kind string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: b}), 0600); err != nil {
		t.Fatal(err)
	}
}
func makeCA(t *testing.T, dir, name string) authority {
	t.Helper()
	key := makeKey(t)
	root := template(t, name)
	root.IsCA = true
	root.BasicConstraintsValid = true
	root.KeyUsage |= x509.KeyUsageCertSign
	der, err := x509.CreateCertificate(rand.Reader, root, root, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".crt")
	savePEM(t, path, "CERTIFICATE", der)
	return authority{cert, key, path}
}
func (ca authority) issue(t *testing.T, dir, name string, server bool) credential {
	t.Helper()
	// The operator makes the key and CSR. The issuer verifies the CSR's proof
	// of possession and signs only the authorized public key and usages.
	key := makeKey(t)
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: name}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(csrBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err = csr.CheckSignature(); err != nil {
		t.Fatal(err)
	}
	cert := template(t, name)
	if server {
		cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, ca.cert, csr.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	scalar, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	id := credential{filepath.Join(dir, name+".crt"), filepath.Join(dir, name+".key"), fmt.Sprintf("%x", sha256.Sum256(der))}
	savePEM(t, id.cert, "CERTIFICATE", der)
	savePEM(t, id.key, "PRIVATE KEY", scalar)
	return id
}
