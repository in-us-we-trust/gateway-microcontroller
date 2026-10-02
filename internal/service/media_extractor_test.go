package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildFakeFFmpeg(t *testing.T, stdout []byte, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	code := fmt.Sprintf("package main\nimport \"os\"\nfunc main() {\n\tos.Stdout.Write(%#v)\n\tos.Exit(%d)\n}\n", stdout, exitCode)
	require.NoError(t, os.WriteFile(src, []byte(code), 0644))
	bin := filepath.Join(dir, "ffmpeg")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return bin
}

func TestFFmpegMediaExtractor_ReturnsStdoutAsImageChunk(t *testing.T) {
	t.Parallel()

	bin := buildFakeFFmpeg(t, []byte("jpeg-bytes"), 0)
	extractor := NewFFmpegMediaExtractor(bin)
	got, err := extractor.ExtractImage(context.Background(), "rtsp://camera/live")
	require.NoError(t, err)
	assert.Equal(t, []byte("jpeg-bytes"), got)
}

func TestFFmpegMediaExtractor_ReturnsStdoutAsAudioAndVideoChunks(t *testing.T) {
	t.Parallel()

	bin := buildFakeFFmpeg(t, []byte("media-bytes"), 0)
	extractor := NewFFmpegMediaExtractor(bin)

	audio, err := extractor.ExtractAudioChunk(context.Background(), "rtsp://camera/live", time.Second, "wav")
	require.NoError(t, err)
	assert.Equal(t, []byte("media-bytes"), audio)

	video, err := extractor.ExtractVideoChunk(context.Background(), "rtsp://camera/live", time.Second)
	require.NoError(t, err)
	assert.Equal(t, []byte("media-bytes"), video)
}

func TestFFmpegMediaExtractor_RejectsInvalidWindows(t *testing.T) {
	t.Parallel()

	extractor := NewFFmpegMediaExtractor("ffmpeg")
	_, err := extractor.ExtractAudioChunk(context.Background(), "rtsp://camera/live", 0, "mp3")
	require.Error(t, err)
	_, err = extractor.ExtractVideoChunk(context.Background(), "rtsp://camera/live", 0)
	require.Error(t, err)
	_, err = extractor.ExtractAudioChunk(context.Background(), "rtsp://camera/live", time.Second, "aac")
	require.Error(t, err)
}

func TestFFmpegMediaExtractor_SurfacesFFmpegFailure(t *testing.T) {
	t.Parallel()

	bin := buildFakeFFmpeg(t, nil, 1)
	extractor := NewFFmpegMediaExtractor(bin)
	_, err := extractor.ExtractImage(context.Background(), "rtsp://camera/live")
	require.Error(t, err)
}

func TestFFmpegMediaExtractor_ExtractImageFromFileRejectsEmptyBuffer(t *testing.T) {
	t.Parallel()

	extractor := NewFFmpegMediaExtractor("ffmpeg")
	_, err := extractor.ExtractImageFromFile(context.Background(), "clip.mp4", nil)
	require.Error(t, err)
}

func TestFFmpegMediaExtractor_ExtractImageFromFileUsesFFmpegStdout(t *testing.T) {
	t.Parallel()

	bin := buildFakeFFmpeg(t, []byte("still-jpeg"), 0)
	extractor := NewFFmpegMediaExtractor(bin)
	got, err := extractor.ExtractImageFromFile(context.Background(), "clip.mp4", []byte("fake-mp4"))
	require.NoError(t, err)
	assert.Equal(t, []byte("still-jpeg"), got)
}

func TestNewFFmpegMediaExtractor_DefaultsExecutable(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ffmpeg", NewFFmpegMediaExtractor("").executable)
}

func TestFFmpegMediaExtractor_RTSPSTLSFlags(t *testing.T) {
	t.Parallel()

	plain := NewFFmpegMediaExtractor("ffmpeg")
	assert.Equal(t, []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-rtsp_transport", "tcp", "-timeout", "10000000",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-i", "rtsp://camera/live",
	}, plain.liveRTSPInputArgs("rtsp://camera/live"))

	secure := NewFFmpegMediaExtractor("ffmpeg").WithRTSPTLS("/certs/ca.crt", true)
	assert.Equal(t, []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-rtsp_transport", "tcp", "-timeout", "10000000",
		"-fflags", "nobuffer", "-flags", "low_delay",
		"-ca_file", "/certs/ca.crt",
		"-tls_verify", "0",
		"-i", "rtsps://gateway:gw-lab@mediamtx:8322/dev/doorbell/track",
	}, secure.liveRTSPInputArgs("rtsps://gateway:gw-lab@mediamtx:8322/dev/doorbell/track"))
}

// buildArgsEchoFFmpeg arma un ffmpeg falso que escribe sus argumentos en stdout.
func buildArgsEchoFFmpeg(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	code := "package main\nimport (\"os\"; \"strings\")\nfunc main() {\n\tos.Stdout.WriteString(strings.Join(os.Args[1:], \" \"))\n}\n"
	require.NoError(t, os.WriteFile(src, []byte(code), 0644))
	bin := filepath.Join(dir, "ffmpeg")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput()
	require.NoError(t, err, string(out))
	return bin
}

func TestFFmpegMediaExtractor_AudioAndVideoAbortOnEmptyOutput(t *testing.T) {
	t.Parallel()

	extractor := NewFFmpegMediaExtractor(buildArgsEchoFFmpeg(t))

	audioArgs, err := extractor.ExtractAudioChunk(context.Background(), "rtsp://camera/live", time.Second, "mp3")
	require.NoError(t, err)
	videoArgs, err := extractor.ExtractVideoChunk(context.Background(), "rtsp://camera/live", time.Second)
	require.NoError(t, err)

	for name, args := range map[string]string{"audio": string(audioArgs), "video": string(videoArgs)} {
		assert.Contains(t, args, "-abort_on empty_output_stream", name)
		assert.Less(t, strings.Index(args, "-abort_on"), strings.Index(args, "pipe:1"), name)
	}
}
