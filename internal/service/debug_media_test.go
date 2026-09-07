package service

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gateway-microcontroller/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDebugMediaWriter_DisabledLogsMetadataWithoutWritingFiles(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	dir := t.TempDir()
	writer := NewDebugMediaWriter(false, dir, 10)
	payload := bytes.Repeat([]byte{0xff, 0xd8}, 64)
	writer.Write(domain.MediaEventRequest{
		TrackID:         "track-1",
		DeviceID:        "device-a",
		MediaType:       domain.MediaTypeVideo,
		FileName:        "chunk_002.mp4",
		Buffer:          payload,
		ChunkNumber:     2,
		ExtractDuration: 51 * time.Millisecond,
		PostDuration:    120 * time.Millisecond,
		Action:          domain.DeviceActionContinue,
	})

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)

	logged := buf.String()
	assert.Contains(t, logged, "trackId=track-1")
	assert.Contains(t, logged, "deviceId=device-a")
	assert.Contains(t, logged, "mediaType=VIDEO")
	assert.Contains(t, logged, "fileName=chunk_002.mp4")
	assert.Contains(t, logged, "bytes=128")
	assert.Contains(t, logged, "chunkNumber=2")
	assert.Contains(t, logged, "extractMs=51")
	assert.Contains(t, logged, "postMs=120")
	assert.Contains(t, logged, "action=CONTINUE")
	assert.NotContains(t, logged, string(payload))
}

func TestNewDebugMediaWriter_EnforcesMaxFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writer := NewDebugMediaWriter(true, dir, 2)

	for i := 1; i <= 4; i++ {
		writer.Write(domain.MediaEventRequest{
			TrackID:     "123",
			MediaType:   domain.MediaTypeVideo,
			FileName:    "chunk.mp4",
			Buffer:      []byte{byte(i)},
			StartedAt:   time.Unix(int64(i), 0).UTC(),
			ChunkNumber: i,
		})
		time.Sleep(15 * time.Millisecond)
	}

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	joined := filepath.Join(names...)
	assert.Contains(t, joined, "chunk-003")
	assert.Contains(t, joined, "chunk-004")
	assert.NotContains(t, joined, "chunk-001")
	assert.NotContains(t, joined, "chunk-002")
}
