package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type MediaExtractor interface {
	ExtractImage(ctx context.Context, rtspURL string) ([]byte, error)
	ExtractImageFromFile(ctx context.Context, fileName string, data []byte) ([]byte, error)
	ExtractAudioChunk(ctx context.Context, rtspURL string, duration time.Duration, format string) ([]byte, error)
	ExtractVideoChunk(ctx context.Context, rtspURL string, duration time.Duration) ([]byte, error)
}

type FFmpegMediaExtractor struct {
	executable   string
	rtspCAFile   string
	rtspInsecure bool
}

func NewFFmpegMediaExtractor(executable string) *FFmpegMediaExtractor {
	if strings.TrimSpace(executable) == "" {
		executable = "ffmpeg"
	}
	return &FFmpegMediaExtractor{executable: executable}
}

func (e *FFmpegMediaExtractor) WithRTSPTLS(caFile string, insecure bool) *FFmpegMediaExtractor {
	if e == nil {
		return e
	}
	e.rtspCAFile = strings.TrimSpace(caFile)
	e.rtspInsecure = insecure
	return e
}

func (e *FFmpegMediaExtractor) ExtractImage(ctx context.Context, rtspURL string) ([]byte, error) {
	args := append(e.liveRTSPInputArgs(rtspURL),
		"-map", "0:v:0",
		"-frames:v", "1",
		"-q:v", "2",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"pipe:1",
	)
	return e.run(ctx, args...)
}

func (e *FFmpegMediaExtractor) ExtractAudioChunk(
	ctx context.Context,
	rtspURL string,
	duration time.Duration,
	format string,
) ([]byte, error) {
	if duration <= 0 {
		return nil, fmt.Errorf("audio chunk duration must be positive")
	}

	args := append(e.liveRTSPInputArgs(rtspURL),
		"-map", "0:a:0",
		"-vn",
		"-t", formatFFmpegDuration(duration),
		"-ac", "1",
		"-ar", "16000",
	)

	switch strings.ToLower(format) {
	case "mp3":
		args = append(args, "-c:a", "libmp3lame", "-b:a", "64k", "-f", "mp3", "pipe:1")
	case "wav":
		args = append(args, "-c:a", "pcm_s16le", "-f", "wav", "pipe:1")
	default:
		return nil, fmt.Errorf("unsupported audio format %q", format)
	}

	return e.run(ctx, args...)
}

func (e *FFmpegMediaExtractor) ExtractVideoChunk(
	ctx context.Context,
	rtspURL string,
	duration time.Duration,
) ([]byte, error) {
	if duration <= 0 {
		return nil, fmt.Errorf("video chunk duration must be positive")
	}

	args := append(e.liveRTSPInputArgs(rtspURL),
		"-map", "0:v:0",
		"-an",
		"-t", formatFFmpegDuration(duration),
		"-vf", "setpts=N/(25*TB)",
		"-fps_mode", "passthrough",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"pipe:1",
	)
	return e.run(ctx, args...)
}

func (e *FFmpegMediaExtractor) run(ctx context.Context, args ...string) ([]byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	command := exec.CommandContext(ctx, e.executable, args...)
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = "no FFmpeg error output"
		}
		return nil, fmt.Errorf("run FFmpeg: %w: %s", err, redactRTSPUserinfo(message))
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("FFmpeg produced an empty media buffer")
	}

	return stdout.Bytes(), nil
}

func (e *FFmpegMediaExtractor) ExtractImageFromFile(ctx context.Context, fileName string, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("video buffer is empty")
	}

	extension := strings.ToLower(filepath.Ext(fileName))
	if extension == "" {
		extension = ".mp4"
	}
	input, err := os.CreateTemp("", "gateway-video-*"+extension)
	if err != nil {
		return nil, fmt.Errorf("create temp video file: %w", err)
	}
	path := input.Name()
	defer os.Remove(path)

	if _, err := input.Write(data); err != nil {
		input.Close()
		return nil, fmt.Errorf("write temp video file: %w", err)
	}
	if err := input.Close(); err != nil {
		return nil, fmt.Errorf("close temp video file: %w", err)
	}

	return e.run(ctx,
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-i", path,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-q:v", "2",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"pipe:1",
	)
}

func (e *FFmpegMediaExtractor) liveRTSPInputArgs(rtspURL string) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-rtsp_transport", "tcp",
		"-timeout", "10000000",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
	}
	if isRTSPS(rtspURL) {
		if e != nil && e.rtspCAFile != "" {
			args = append(args, "-ca_file", e.rtspCAFile)
		}
		if e != nil && e.rtspInsecure {
			args = append(args, "-tls_verify", "0")
		}
	}
	return append(args, "-i", rtspURL)
}

func isRTSPS(rtspURL string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(rtspURL)), "rtsps://")
}

func formatFFmpegDuration(duration time.Duration) string {
	return fmt.Sprintf("%.3f", duration.Seconds())
}
