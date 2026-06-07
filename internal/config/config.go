package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port               string
	DetectorServiceURL string
	AuthServiceURL     string
	TLS                TLSConfig
}

func Load() Config {
	if err := godotenv.Load(); err != nil {
		log.Println("[config] no .env file found, reading from environment")
	}

	return Config{
		Port:               getEnv("PORT", "8080"),
		DetectorServiceURL: getEnv("DETECTOR_SERVICE_URL", "http://localhost:8081"),
		AuthServiceURL:     getEnv("AUTH_SERVICE_URL", "http://localhost:8082"),
		TLS: TLSConfig{
			ServerCertPEM: getEnv("SERVER_CERT_PEM", ""),
			ServerKeyPEM:  getEnv("SERVER_KEY_PEM", ""),
			ClientCAPEM:   getEnv("CLIENT_CA_PEM", ""),
		},
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
