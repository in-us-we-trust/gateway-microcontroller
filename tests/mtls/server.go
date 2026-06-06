package mtls_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway-microcontroller/internal/handler"
	"gateway-microcontroller/internal/middleware"
	"gateway-microcontroller/internal/service"
	"gateway-microcontroller/tests/testutil"

	"github.com/gin-gonic/gin"
)

type mtlsServer struct {
	Server   *httptest.Server
	CA       *caBundle
	Auth     *testutil.MockAuthClient
	Detector *testutil.MockDetectorClient
}

func newMTLSServer(t *testing.T) *mtlsServer {
	t.Helper()
	return startServer(t, true)
}

func newHTTPServer(t *testing.T) *mtlsServer {
	t.Helper()
	return startServer(t, false)
}

func startServer(t *testing.T, withTLS bool) *mtlsServer {
	t.Helper()
	gin.SetMode(gin.TestMode)

	auth := new(testutil.MockAuthClient)
	detector := new(testutil.MockDetectorClient)

	deviceAuth := service.NewDeviceAuthService(auth)
	mtls := middleware.NewMTLSMiddleware(deviceAuth)
	eventHandler := handler.NewEventHandler(service.NewEventService(detector))

	router := gin.New()
	api := router.Group("/api/v1")
	api.Use(mtls.Handle())
	api.POST("/events", eventHandler.PostEvent)

	ca := newCA(t)
	var srv *httptest.Server

	if withTLS {
		serverCert := ca.issueServerCert(t)
		srv = httptest.NewUnstartedServer(router)
		srv.TLS = &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    ca.Pool,
			MinVersion:   tls.VersionTLS12,
		}
		srv.StartTLS()
	} else {
		srv = httptest.NewServer(router)
	}

	return &mtlsServer{
		Server:   srv,
		CA:       ca,
		Auth:     auth,
		Detector: detector,
	}
}

func tlsHTTPClient(ca *caBundle, clientCert tls.Certificate) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{clientCert},
				RootCAs:      ca.Pool,
				MinVersion:   tls.VersionTLS12,
			},
		},
	}
}

func tlsHTTPClientWithoutCert(ca *caBundle) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    ca.Pool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}
}
