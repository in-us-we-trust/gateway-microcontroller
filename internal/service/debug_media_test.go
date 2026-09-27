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

func TestDebugMediaWriter_CaptureImagesDoNotOverwritePressSnapshot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writer := NewDebugMediaWriter(true, dir, 10)
	startedAt := time.Date(2026, 9, 27, 17, 0, 0, 0, time.UTC)

	writer.Write(domain.MediaEventRequest{
		TrackID:     "track-1",
		MediaType:   domain.MediaTypeImage,
		FileName:    "snapshot.jpeg",
		Buffer:      []byte("press"),
		StartedAt:   startedAt,
		ChunkNumber: 1,
	})
	writer.Write(domain.MediaEventRequest{
		TrackID:   "track-1",
		MediaType: domain.MediaTypeImage,
		FileName:  "snapshot.jpeg",
		Buffer:    []byte("capture-1"),
		StartedAt: startedAt,
		DebugKind: "capture",
	})
	writer.Write(domain.MediaEventRequest{
		TrackID:   "track-1",
		MediaType: domain.MediaTypeImage,
		FileName:  "snapshot.jpeg",
		Buffer:    []byte("capture-2"),
		StartedAt: startedAt,
		DebugKind: "capture",
	})

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 3)

	press, err := os.ReadFile(filepath.Join(dir, "event-track-1_20260927-170000_image_chunk-001.jpg"))
	require.NoError(t, err)
	assert.Equal(t, []byte("press"), press)

	first, err := os.ReadFile(filepath.Join(dir, "event-track-1_20260927-170000_capture_chunk-001.jpg"))
	require.NoError(t, err)
	assert.Equal(t, []byte("capture-1"), first)

	second, err := os.ReadFile(filepath.Join(dir, "event-track-1_20260927-170000_capture_chunk-002.jpg"))
	require.NoError(t, err)
	assert.Equal(t, []byte("capture-2"), second)
}
