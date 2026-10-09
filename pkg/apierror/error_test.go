package apierror

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestFromDetectorError_UsesUpstreamStatus(t *testing.T) {
	t.Parallel()

	err := FromDetectorError("orchestrator down", "/api/v1/events", "DTE-001", http.StatusBadGateway)
	assert.Equal(t, http.StatusBadGateway, err.Status)
	assert.Equal(t, "DTE-001", err.Code)
	assert.Equal(t, "orchestrator down", err.Detail)

	fallback := FromDetectorError("x", "/y", "DTE-001", 599)
	assert.Equal(t, "Upstream Error", fallback.Title)
}

func TestRespondWritesProblemDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/events", nil)

	Respond(c, BadGateway("upstream failed", "/api/v1/events", "DTE-002"))
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "DTE-002")
	assert.Contains(t, w.Body.String(), "Bad Gateway")

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/events", nil)

	Respond(c, Unauthorized("missing device", "/api/v1/events"))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	Respond(c, BadRequest("invalid payload", "/api/v1/events"))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	Respond(c, InternalServerError("boom", "/api/v1/events"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
