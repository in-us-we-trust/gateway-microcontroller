package apierror

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIError represents an RFC 7807 Problem Details error response.
type APIError struct {
	Status   int    `json:"status"   example:"400"`
	Title    string `json:"title"    example:"Bad Request"`
	Detail   string `json:"detail"   example:"The image field is required"`
	Instance string `json:"instance" example:"/api/v1/events"`
	Type     string `json:"type"     example:"about:blank"`
	Code     string `json:"code,omitempty" example:"DTE-001"`
}

func New(status int, title, detail, instance string) *APIError {
	return &APIError{
		Status:   status,
		Title:    title,
		Detail:   detail,
		Instance: instance,
		Type:     "about:blank",
	}
}

func BadRequest(detail, instance string) *APIError {
	return New(http.StatusBadRequest, "Bad Request", detail, instance)
}

func Unauthorized(detail, instance string) *APIError {
	return New(http.StatusUnauthorized, "Unauthorized", detail, instance)
}

func BadGateway(detail, instance, code string) *APIError {
	err := New(http.StatusBadGateway, "Bad Gateway", detail, instance)
	err.Code = code
	return err
}

func FromDetectorError(detail, instance, code string, statusCode int) *APIError {
	title := http.StatusText(statusCode)
	if title == "" {
		title = "Upstream Error"
	}
	err := New(statusCode, title, detail, instance)
	err.Code = code
	return err
}

func InternalServerError(detail, instance string) *APIError {
	return New(http.StatusInternalServerError, "Internal Server Error", detail, instance)
}

func Respond(c *gin.Context, err *APIError) {
	c.JSON(err.Status, err)
}
