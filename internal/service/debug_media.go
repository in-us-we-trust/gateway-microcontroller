package service

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"gateway-microcontroller/internal/domain"
)

const defaultDebugMediaDir = "debug-chunks"
const defaultDebugMediaMaxFiles = 20

type DebugMediaWriter interface {
	Write(req domain.MediaEventRequest)
}

type noDebugMediaWriter struct{}

func (noDebugMediaWriter) Write(req domain.MediaEventRequest) {
	log.Printf(
		"media dispatch trackId=%s deviceId=%s mediaType=%s fileName=%s bytes=%d chunkNumber=%d extractMs=%d postMs=%d action=%s",
		req.TrackID,
		req.DeviceID,
		req.MediaType,
		req.FileName,
		len(req.Buffer),
		req.ChunkNumber,
		req.ExtractDuration.Milliseconds(),
		req.PostDuration.Milliseconds(),
		req.Action,
	)
}

func NewDebugMediaWriter(enabled bool, dir string, maxFiles int) DebugMediaWriter {
	logger := noDebugMediaWriter{}
	if !enabled {
		return logger
	}

	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = defaultDebugMediaDir
	}
	if maxFiles <= 0 {
		maxFiles = defaultDebugMediaMaxFiles
	}

	return &fileDebugMediaWriter{
		logger:   logger,
		dir:      dir,
		maxFiles: maxFiles,
		chunks:   make(map[string]int),
	}
}

type fileDebugMediaWriter struct {
	logger   noDebugMediaWriter
	dir      string
	maxFiles int

	mu     sync.Mutex
	chunks map[string]int
}

func (w *fileDebugMediaWriter) Write(req domain.MediaEventRequest) {
	w.logger.Write(req)
	if len(req.Buffer) == 0 {
		return
	}

	kind, ext := debugMediaKind(req)
	startedAt := req.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	chunkNumber := req.ChunkNumber
	if chunkNumber <= 0 {
		key := req.TrackID + "|" + kind
		w.chunks[key]++
		chunkNumber = w.chunks[key]
	}

	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		log.Printf("debug media: create dir %s: %v", w.dir, err)
		return
	}
	if err := w.enforceMaxFilesLocked(); err != nil {
		log.Printf("debug media: rotate %s: %v", w.dir, err)
		return
	}

	name := debugMediaFileName(req.TrackID, startedAt, kind, chunkNumber, ext)
	path := filepath.Join(w.dir, name)
	if err := os.WriteFile(path, req.Buffer, 0o644); err != nil {
		log.Printf("debug media: write %s: %v", path, err)
		return
	}

	log.Printf("debug media saved %s (%d bytes)", path, len(req.Buffer))
}

func (w *fileDebugMediaWriter) enforceMaxFilesLocked() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}

	type datedFile struct {
		path    string
		modTime time.Time
	}

	files := make([]datedFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, datedFile{
			path:    filepath.Join(w.dir, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	for len(files) >= w.maxFiles {
		if err := os.Remove(files[0].path); err != nil && !os.IsNotExist(err) {
			return err
		}
		files = files[1:]
	}
	return nil
}

func debugMediaKind(req domain.MediaEventRequest) (string, string) {
	switch req.MediaType {
	case domain.MediaTypeVideo:
		return "video", "mp4"
	case domain.MediaTypeAudio:
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(req.FileName)), ".")
		if ext == "" {
			ext = "wav"
		}
		return "audio", ext
	default:
		return "image", "jpg"
	}
}

func debugMediaFileName(eventID string, startedAt time.Time, kind string, chunkNumber int, ext string) string {
	id := sanitizeDebugToken(eventID)
	if id == "" {
		id = "unknown"
	}
	if !strings.HasPrefix(id, "event-") {
		id = "event-" + id
	}
	return fmt.Sprintf(
		"%s_%s_%s_chunk-%03d.%s",
		id,
		startedAt.UTC().Format("20060102-150405"),
		kind,
		chunkNumber,
		ext,
	)
}

func sanitizeDebugToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
