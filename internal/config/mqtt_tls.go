package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"strings"
)

func MQTTUsesTLS(brokerURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(brokerURL))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "ssl", "tls", "mqtts":
		return true
	default:
		return false
	}
}

func (c Config) MQTTTLSConfig() (*tls.Config, error) {
	if !MQTTUsesTLS(c.MQTTBrokerURL) {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if c.MQTTTLSInsecure {
		tlsCfg.InsecureSkipVerify = true
	}

	caPEM, err := loadPEM(c.MQTTCACert, "MQTT_CA_CERT")
	if err != nil {
		return nil, err
	}
	if len(caPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("MQTT_CA_CERT does not contain a valid PEM certificate")
		}
		tlsCfg.RootCAs = pool
	}

	certPEM, err := loadPEM(c.MQTTClientCert, "MQTT_CLIENT_CERT")
	if err != nil {
		return nil, err
	}
	keyPEM, err := loadPEM(c.MQTTClientKey, "MQTT_CLIENT_KEY")
	if err != nil {
		return nil, err
	}
	if len(certPEM) == 0 && len(keyPEM) == 0 {
		return tlsCfg, nil
	}
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return nil, fmt.Errorf("MQTT_CLIENT_CERT and MQTT_CLIENT_KEY must both be set")
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load MQTT client certificate: %w", err)
	}
	tlsCfg.Certificates = []tls.Certificate{cert}
	return tlsCfg, nil
}

func loadPEM(value, label string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if strings.Contains(value, "-----BEGIN ") {
		return []byte(value), nil
	}
	data, err := os.ReadFile(value)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	return data, nil
}
