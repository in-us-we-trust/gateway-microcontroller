package client

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadAndRestoreBody(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "<empty>", readAndRestoreBody(nil))

	var empty io.ReadCloser
	assert.Equal(t, "<empty>", readAndRestoreBody(&empty))

	blank := io.NopCloser(bytes.NewReader(nil))
	assert.Equal(t, "<empty>", readAndRestoreBody(&blank))

	broken := io.NopCloser(errReader{})
	assert.Equal(t, "<unreadable>", readAndRestoreBody(&broken))

	body := io.NopCloser(bytes.NewReader([]byte(`{"ok":true}`)))
	assert.Equal(t, `{"ok":true}`, readAndRestoreBody(&body))
	restored, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, string(restored))
}

func TestLoggingRoundTripper_LogsTransportError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	_, err = (&loggingRoundTripper{next: http.DefaultTransport}).RoundTrip(req)
	require.Error(t, err)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
