package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gateway-microcontroller/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDebugMediaWriter_DisabledDoesNotWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writer := NewDebugMediaWriter(false, dir, 10)
	writer.Write(domain.MediaEventRequest{
		TrackID:   "123",
		MediaType: domain.MediaTypeImage,
		FileName:  "snapshot.jpeg",
		Buffer:    []byte("jpeg"),
		StartedAt: time.Now(),
	})

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
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
