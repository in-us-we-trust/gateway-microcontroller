package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMQTTUsesTLS(t *testing.T) {
	t.Parallel()

	assert.False(t, MQTTUsesTLS("tcp://localhost:1883"))
	assert.True(t, MQTTUsesTLS("ssl://localhost:8883"))
	assert.True(t, MQTTUsesTLS("tls://broker.example:8883"))
	assert.True(t, MQTTUsesTLS("mqtts://broker.example:8883"))
	assert.False(t, MQTTUsesTLS("mqtt://localhost:1883"))
}

func TestMQTTTLSConfig_NilForTCP(t *testing.T) {
	t.Parallel()

	cfg := Config{MQTTBrokerURL: "tcp://localhost:1883", MQTTCACert: "/nope.crt"}
	got, err := cfg.MQTTTLSConfig()
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestMQTTTLSConfig_SystemRootsWhenNoCA(t *testing.T) {
	t.Parallel()

	cfg := Config{MQTTBrokerURL: "ssl://localhost:8883"}
	got, err := cfg.MQTTTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, got.RootCAs)
	assert.False(t, got.InsecureSkipVerify)
	assert.Empty(t, got.Certificates)
}

func TestMQTTTLSConfig_InsecureSkipVerify(t *testing.T) {
	t.Parallel()

	cfg := Config{MQTTBrokerURL: "ssl://localhost:8883", MQTTTLSInsecure: true}
	got, err := cfg.MQTTTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.InsecureSkipVerify)
}

func TestMQTTTLSConfig_LoadsCAAndClientCertFromFiles(t *testing.T) {
	t.Parallel()

	caPEM, certPEM, keyPEM := testGatewayCerts(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.crt")
	certPath := filepath.Join(dir, "client.crt")
	keyPath := filepath.Join(dir, "client.key")
	require.NoError(t, os.WriteFile(caPath, caPEM, 0o600))
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))

	cfg := Config{
		MQTTBrokerURL:  "ssl://localhost:8883",
		MQTTCACert:     caPath,
		MQTTClientCert: certPath,
		MQTTClientKey:  keyPath,
	}
	got, err := cfg.MQTTTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.RootCAs)
	require.Len(t, got.Certificates, 1)
}

func TestMQTTTLSConfig_LoadsPEMInline(t *testing.T) {
	t.Parallel()

	caPEM, certPEM, keyPEM := testGatewayCerts(t)
	cfg := Config{
		MQTTBrokerURL:  "ssl://localhost:8883",
		MQTTCACert:     string(caPEM),
		MQTTClientCert: string(certPEM),
		MQTTClientKey:  string(keyPEM),
	}
	got, err := cfg.MQTTTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Certificates, 1)
}

func TestMQTTTLSConfig_RequiresCertAndKeyTogether(t *testing.T) {
	t.Parallel()

	_, certPEM, _ := testGatewayCerts(t)
	cfg := Config{
		MQTTBrokerURL:  "ssl://localhost:8883",
		MQTTClientCert: string(certPEM),
	}
	_, err := cfg.MQTTTLSConfig()
	require.Error(t, err)
}

func TestMQTTTLSConfig_RejectsMissingFile(t *testing.T) {
	t.Parallel()

	cfg := Config{
		MQTTBrokerURL: "ssl://localhost:8883",
		MQTTCACert:    filepath.Join(t.TempDir(), "missing.crt"),
	}
	_, err := cfg.MQTTTLSConfig()
	require.Error(t, err)
}

func testGatewayCerts(t *testing.T) (caPEM, certPEM, keyPEM []byte) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	clientTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "gateway-micro"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTmpl, caTmpl, &clientKey.PublicKey, caKey)
	require.NoError(t, err)

	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	require.NoError(t, err)

	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyDER})
	return caPEM, certPEM, keyPEM
}
