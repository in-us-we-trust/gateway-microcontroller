package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/requestctx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPDetectorClient_Analyze_SendsDeviceIDInPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/devices/device-abc/events", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(domain.EventResponse{
			Data: domain.EventData{
				EventID:    "event-123",
				DeviceID:   "device-abc",
				IsIntruder: true,
			},
		}))
	}))
	defer server.Close()

	c := NewHTTPDetectorClient(server.URL)
	ctx := requestctx.WithDeviceID(context.Background(), "device-abc")

	resp, err := c.Analyze(ctx, &domain.EventRequest{
		Type:  domain.EventTypeMotion,
		Image: "base64data",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "event-123", resp.EventID)
	assert.Equal(t, "device-abc", resp.DeviceID)
	assert.True(t, resp.IsIntruder)
}

func TestHTTPDetectorClient_SendMediaEvent_SendsMultipartPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mediaType   domain.MediaType
		fileName    string
		contentType string
		buffer      []byte
	}{
		{
			name:        "image",
			mediaType:   domain.MediaTypeImage,
			fileName:    "snapshot.jpeg",
			contentType: "image/jpeg",
			buffer:      []byte{0xff, 0xd8, 0xff, 0xd9},
		},
		{
			name:        "audio",
			mediaType:   domain.MediaTypeAudio,
			fileName:    "chunk_001.mp3",
			contentType: "audio/mpeg",
			buffer:      []byte("audio-data"),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, orchestratorEventsPath, r.URL.Path)
				require.NoError(t, r.ParseMultipartForm(1<<20))
				assert.Equal(t, "track-123", r.FormValue("trackId"))
				assert.Equal(t, "device-abc", r.FormValue("deviceId"))
				assert.Equal(t, string(tt.mediaType), r.FormValue("mediaType"))

				file, header, err := r.FormFile("file")
				require.NoError(t, err)
				defer file.Close()

				actualBuffer, err := io.ReadAll(file)
				require.NoError(t, err)
				assert.Equal(t, tt.fileName, header.Filename)
				assert.Equal(t, tt.contentType, header.Header.Get("Content-Type"))
				assert.Equal(t, tt.buffer, actualBuffer)

				w.Header().Set("Content-Type", "application/json")
				require.NoError(t, json.NewEncoder(w).Encode(domain.OrchestratorEventResponse{
					Data: domain.OrchestratorEventData{
						TrackID:       "track-123",
						ChunkType:     string(tt.mediaType),
						FinalVerdict:  domain.DeviceActionContinue,
						FinalSeverity: "LOW",
						Message:       "No danger.",
					},
				}))
			}))
			defer server.Close()

			detector := NewHTTPDetectorClient(server.URL)
			resp, err := detector.SendMediaEvent(context.Background(), domain.MediaEventRequest{
				TrackID:   "track-123",
				DeviceID:  "device-abc",
				MediaType: tt.mediaType,
				FileName:  tt.fileName,
				Buffer:    tt.buffer,
			})

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, "track-123", resp.TrackID)
			assert.Equal(t, string(tt.mediaType), resp.ChunkType)
		})
	}
}

func TestHTTPDetectorClient_RegisterDevice(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/devices", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(domain.DeviceResponse{
			Data: domain.DeviceData{ID: "device-abc", SerialNumber: "SN-1"},
		}))
	}))
	defer server.Close()

	c := NewHTTPDetectorClient(server.URL)
	resp, err := c.RegisterDevice(context.Background(), &domain.DeviceRequest{
		ID:           "device-abc",
		SerialNumber: "SN-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "device-abc", resp.Data.ID)
}

func TestHTTPDetectorClient_AnalyzeRequiresDeviceInContext(t *testing.T) {
	t.Parallel()

	c := NewHTTPDetectorClient("http://example")
	_, err := c.Analyze(context.Background(), &domain.EventRequest{Type: domain.EventTypeBell, Image: "x"})
	require.Error(t, err)
}

func TestHTTPDetectorClient_SendMediaEventRequiresIdentityAndBuffer(t *testing.T) {
	t.Parallel()

	c := NewHTTPDetectorClient("http://example")
	_, err := c.SendMediaEvent(context.Background(), domain.MediaEventRequest{
		MediaType: domain.MediaTypeImage, FileName: "snapshot.jpeg", Buffer: []byte("x"),
	})
	require.Error(t, err)

	_, err = c.SendMediaEvent(context.Background(), domain.MediaEventRequest{
		TrackID: "t", DeviceID: "d", MediaType: domain.MediaTypeImage, FileName: "snapshot.jpeg",
	})
	require.Error(t, err)
}

func TestHTTPDetectorClient_SendMediaEventRejectsUnsupportedType(t *testing.T) {
	t.Parallel()

	c := NewHTTPDetectorClient("http://example")
	_, err := c.SendMediaEvent(context.Background(), domain.MediaEventRequest{
		TrackID: "t", DeviceID: "d", MediaType: "TEXT", FileName: "note.txt", Buffer: []byte("x"),
	})
	require.Error(t, err)
}

func TestHTTPDetectorClient_SendMediaEventVideo(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(1<<20))
		assert.Equal(t, "VIDEO", r.FormValue("mediaType"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(domain.OrchestratorEventResponse{
			Data: domain.OrchestratorEventData{TrackID: "track-123", ChunkType: "VIDEO"},
		}))
	}))
	defer server.Close()

	detector := NewHTTPDetectorClient(server.URL)
	resp, err := detector.SendMediaEvent(context.Background(), domain.MediaEventRequest{
		TrackID: "track-123", DeviceID: "device-abc", MediaType: domain.MediaTypeVideo,
		FileName: "chunk_001.mp4", Buffer: []byte("mp4"),
	})
	require.NoError(t, err)
	assert.Equal(t, "VIDEO", resp.ChunkType)
}

func TestHTTPDetectorClient_SendMediaEventMapsUpstreamError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()

	detector := NewHTTPDetectorClient(server.URL)
	_, err := detector.SendMediaEvent(context.Background(), domain.MediaEventRequest{
		TrackID: "t", DeviceID: "d", MediaType: domain.MediaTypeImage,
		FileName: "snapshot.jpeg", Buffer: []byte("jpeg"),
	})
	require.Error(t, err)
}
