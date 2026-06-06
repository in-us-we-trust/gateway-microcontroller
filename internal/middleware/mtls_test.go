package middleware_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/middleware"
	"gateway-microcontroller/internal/requestctx"
	"gateway-microcontroller/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockAuthClient struct {
	mock.Mock
}

func (m *mockAuthClient) ValidateDevice(ctx context.Context, deviceID, serial string) (bool, error) {
	args := m.Called(ctx, deviceID, serial)
	return args.Bool(0), args.Error(1)
}

var _ client.AuthClient = (*mockAuthClient)(nil)

func testClientCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

func setupMiddlewareRouter(t *testing.T, auth client.AuthClient) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	deviceAuth := service.NewDeviceAuthService(auth)
	mtls := middleware.NewMTLSMiddleware(deviceAuth)

	r := gin.New()
	r.Use(mtls.Handle())
	r.GET("/protected", func(c *gin.Context) {
		device, ok := requestctx.GetDeviceContext(c.Request.Context())
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "missing device context"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"deviceId": device.DeviceID, "serial": device.Serial})
	})
	return r
}

func serveWithTLS(t *testing.T, router *gin.Engine, certs []*x509.Certificate) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if certs != nil {
		req.TLS = &tls.ConnectionState{PeerCertificates: certs}
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestMTLSMiddleware_TLSRequired(t *testing.T) {
	t.Parallel()

	router := setupMiddlewareRouter(t, new(mockAuthClient))
	w := serveWithTLS(t, router, nil)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "tls required", body["error"])
}

func TestMTLSMiddleware_ClientCertificateRequired(t *testing.T) {
	t.Parallel()

	router := setupMiddlewareRouter(t, new(mockAuthClient))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: nil}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "client certificate required", body["error"])
}

func TestMTLSMiddleware_DeviceIDNotFoundInCertificate(t *testing.T) {
	t.Parallel()

	auth := new(mockAuthClient)
	router := setupMiddlewareRouter(t, auth)

	cert := testClientCert(t, "")
	w := serveWithTLS(t, router, []*x509.Certificate{cert})

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "device id not found in certificate", body["error"])

	auth.AssertNotCalled(t, "ValidateDevice")
}

func TestMTLSMiddleware_DeviceNotAuthorized(t *testing.T) {
	t.Parallel()

	auth := new(mockAuthClient)
	router := setupMiddlewareRouter(t, auth)

	cert := testClientCert(t, "device-456")
	auth.On("ValidateDevice", mock.Anything, "device-456", cert.SerialNumber.String()).
		Return(false, nil)

	w := serveWithTLS(t, router, []*x509.Certificate{cert})

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "device not authorized", body["error"])
}

func TestMTLSMiddleware_ValidCertificateInjectsDeviceContext(t *testing.T) {
	t.Parallel()

	auth := new(mockAuthClient)
	router := setupMiddlewareRouter(t, auth)

	cert := testClientCert(t, "device-789")
	auth.On("ValidateDevice", mock.Anything, "device-789", cert.SerialNumber.String()).
		Return(true, nil)

	w := serveWithTLS(t, router, []*x509.Certificate{cert})

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "device-789", body["deviceId"])
	assert.Equal(t, cert.SerialNumber.String(), body["serial"])
}
