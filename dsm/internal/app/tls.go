package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func ensureCertificate(dir string) error {
	certPath, keyPath := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
	if _, e := os.Stat(certPath); e == nil {
		if _, e = os.Stat(keyPath); e == nil {
			return nil
		}
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return e
	}
	host, _ := os.Hostname()
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "USB UPS Server"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(2, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost", host}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	if addrs, e := net.InterfaceAddrs(); e == nil {
		for _, a := range addrs {
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil {
				template.IPAddresses = append(template.IPAddresses, ip)
			}
		}
	}
	der, e := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if e != nil {
		return e
	}
	encoded, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return e
	}
	if e = atomicWrite(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})); e != nil {
		return e
	}
	return atomicWrite(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
