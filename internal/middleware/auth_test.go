package middleware

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway-microcontroller/internal/requestctx"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSubjectHeader(deviceID string) string {
	name := pkix.Name{CommonName: deviceID}
	der, err := asn1.Marshal(name.ToRDNSequence())
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func TestAuth_LegacyHeaderIdentifiesDevice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Auth(ExtractDeviceIDFromHeader))
	router.GET("/ping", func(c *gin.Context) {
		deviceID, ok := requestctx.DeviceID(c.Request.Context())
		if !ok {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.String(http.StatusOK, deviceID)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Device-ID", "device-header")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "device-header", w.Body.String())
}

func TestAuth_MissingDeviceHeaderIsUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Auth(ExtractDeviceIDFromHeader))
	router.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestExtractDeviceIDFromMTLS_RequiresCertificateSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	_, err := ExtractDeviceIDFromMTLS(c)
	require.Error(t, err)
}

func TestAuth_MTLSCertificateCNIdentifiesDevice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Auth(ExtractDeviceIDFromMTLS))
	router.GET("/ping", func(c *gin.Context) {
		deviceID, ok := requestctx.DeviceID(c.Request.Context())
		if !ok {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.String(http.StatusOK, deviceID)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-mtls"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "device-mtls", w.Body.String())
}
