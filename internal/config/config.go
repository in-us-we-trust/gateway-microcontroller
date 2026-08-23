package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                string
	DetectiveManagerURL string
	EnableMTLS          bool
	MQTTEnabled         bool
	MQTTBrokerURL       string
	MQTTClientID        string
	MQTTUsername        string
	MQTTPassword        string
	SessionTTL          time.Duration
	AudioChunkDuration  time.Duration
	VideoChunkDuration  time.Duration
	AudioFormat         string
	FFmpegPath          string
	MediaRequestTimeout time.Duration
	MediaChunkBatchSize int
	DebugMediaDir       string
	DebugMediaEnabled   bool
	DebugMediaMaxFiles  int
}

func Load() Config {
	if err := godotenv.Load(); err != nil {
		log.Println("[config] no .env file found, reading from environment")
	}

	return Config{
		Port:                getEnv("PORT", "8080"),
		DetectiveManagerURL: getEnv("DETECTIVE_MANAGER_URL", getEnv("DETECTOR_SERVICE_URL", "http://localhost:8081")),
		EnableMTLS:          getEnvBool("ENABLE_MTLS", true),
		MQTTEnabled:         getEnvBool("MQTT_ENABLED", true),
		MQTTBrokerURL:       getEnv("MQTT_BROKER_URL", "tcp://localhost:1883"),
		MQTTClientID:        getEnv("MQTT_CLIENT_ID", "gateway-micro"),
		MQTTUsername:        getEnv("MQTT_USERNAME", ""),
		MQTTPassword:        getEnv("MQTT_PASSWORD", ""),
		SessionTTL:          getEnvDuration("SESSION_TTL", 30*time.Second),
		AudioChunkDuration:  getEnvDuration("AUDIO_CHUNK_DURATION", 10*time.Second),
		VideoChunkDuration:  getEnvDuration("VIDEO_CHUNK_DURATION", 5*time.Second),
		AudioFormat:         getEnv("AUDIO_FORMAT", "mp3"),
		FFmpegPath:          getEnv("FFMPEG_PATH", "ffmpeg"),
		MediaRequestTimeout: getEnvDuration("MEDIA_REQUEST_TIMEOUT", 15*time.Second),
		MediaChunkBatchSize: getEnvInt("MEDIA_CHUNK_BATCH_SIZE", 1),
		DebugMediaDir:       getEnv("DEBUG_MEDIA_DIR", ""),
		DebugMediaEnabled:   getEnvBoolExplicit("DEBUG_MEDIA_ENABLED"),
		DebugMediaMaxFiles:  getEnvInt("DEBUG_MEDIA_MAX_FILES", 0),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("[config] invalid boolean %s=%q, using %t", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvBoolExplicit(key string) bool {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return false
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Printf("[config] invalid boolean %s=%q, debug media disabled", key, value)
		return false
	}
	return parsed
}

func getEnvInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		log.Printf("[config] invalid integer %s=%q, using %d", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		log.Printf("[config] invalid duration %s=%q, using %s", key, value, fallback)
		return fallback
	}
	return parsed
}
