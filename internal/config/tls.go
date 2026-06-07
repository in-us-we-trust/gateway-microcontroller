package config

type TLSConfig struct {
	ServerCertPEM string
	ServerKeyPEM  string
	ClientCAPEM   string
}
