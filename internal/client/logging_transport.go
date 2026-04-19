package client

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"time"
)

type loggingRoundTripper struct {
	next http.RoundTripper
}

func (l *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	reqBody := readAndRestoreBody(&req.Body)

	resp, err := l.next.RoundTrip(req)

	if err != nil {
		log.Printf("[outgoing] %s %s body=%s -> error: %v (%s)",
			req.Method, req.URL, reqBody, err, time.Since(start))
		return nil, err
	}

	respBody := readAndRestoreBody(&resp.Body)

	log.Printf("[outgoing] %s %s body=%s -> %d body=%s (%s)",
		req.Method, req.URL, reqBody, resp.StatusCode, respBody, time.Since(start))

	return resp, nil
}

func readAndRestoreBody(rc *io.ReadCloser) string {
	if rc == nil || *rc == nil {
		return "<empty>"
	}
	data, err := io.ReadAll(*rc)
	if err != nil {
		return "<unreadable>"
	}
	(*rc).Close()
	*rc = io.NopCloser(bytes.NewReader(data))
	if len(data) == 0 {
		return "<empty>"
	}
	return string(data)
}
