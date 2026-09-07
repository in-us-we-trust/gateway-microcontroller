package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLoad_UsesDocumentedDefaultsWithoutEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())

	cfg := Load()
	assert.Equal(t, 30*time.Second, cfg.SessionTTL)
	assert.Equal(t, 10*time.Second, cfg.AudioChunkDuration)
	assert.Equal(t, 5*time.Second, cfg.VideoChunkDuration)
	assert.Equal(t, "mp3", cfg.AudioFormat)
	assert.Equal(t, 15*time.Second, cfg.MediaRequestTimeout)
	assert.Equal(t, "rtsp://localhost:8554", cfg.RTSPBase)
	assert.False(t, cfg.DebugMediaEnabled)
	assert.Equal(t, "gateway-micro", cfg.MQTTClientID)
	assert.Equal(t, "tcp://localhost:1883", cfg.MQTTBrokerURL)
}

func TestLoad_OperatorEnvConfiguresRTSPAndSession(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("RTSP_BASE", "rtsp://mediamtx:8554")
	t.Setenv("SESSION_TTL", "2m")
	t.Setenv("MQTT_ENABLED", "false")
	t.Setenv("DEBUG_MEDIA_ENABLED", "true")
	t.Setenv("DEBUG_MEDIA_MAX_FILES", "5")
	t.Setenv("MEDIA_CHUNK_BATCH_SIZE", "3")
	t.Setenv("ENABLE_MTLS", "false")
	t.Setenv("DETECTIVE_MANAGER_URL", "http://dm.example")

	cfg := Load()
	assert.Equal(t, "rtsp://mediamtx:8554", cfg.RTSPBase)
	assert.Equal(t, 2*time.Minute, cfg.SessionTTL)
	assert.False(t, cfg.MQTTEnabled)
	assert.True(t, cfg.DebugMediaEnabled)
	assert.Equal(t, 5, cfg.DebugMediaMaxFiles)
	assert.Equal(t, 3, cfg.MediaChunkBatchSize)
	assert.False(t, cfg.EnableMTLS)
	assert.Equal(t, "http://dm.example", cfg.DetectiveManagerURL)
}

func TestLoad_InvalidEnvKeepsSafeDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SESSION_TTL", "nope")
	t.Setenv("ENABLE_MTLS", "maybe")
	t.Setenv("DEBUG_MEDIA_ENABLED", "maybe")
	t.Setenv("MEDIA_CHUNK_BATCH_SIZE", "-1")

	cfg := Load()
	assert.Equal(t, 30*time.Second, cfg.SessionTTL)
	assert.True(t, cfg.EnableMTLS)
	assert.False(t, cfg.DebugMediaEnabled)
	assert.Equal(t, 1, cfg.MediaChunkBatchSize)
}
