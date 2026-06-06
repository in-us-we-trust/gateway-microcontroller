package service_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/service"

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

func testCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
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

func TestDeviceAuthService_ValidateCertificate_Success(t *testing.T) {
	t.Parallel()

	auth := new(mockAuthClient)
	svc := service.NewDeviceAuthService(auth)
	cert := testCert(t, "device-001")

	auth.On("ValidateDevice", mock.Anything, "device-001", cert.SerialNumber.String()).
		Return(true, nil)

	device, err := svc.ValidateCertificate(context.Background(), cert)
	require.NoError(t, err)
	assert.Equal(t, "device-001", device.DeviceID)
	assert.Equal(t, cert.SerialNumber.String(), device.Serial)
}

func TestDeviceAuthService_ValidateCertificate_EmptyCN(t *testing.T) {
	t.Parallel()

	svc := service.NewDeviceAuthService(new(mockAuthClient))
	cert := testCert(t, "")

	_, err := svc.ValidateCertificate(context.Background(), cert)
	assert.EqualError(t, err, "device id not found in certificate")
}

func TestDeviceAuthService_ValidateCertificate_NotAuthorized(t *testing.T) {
	t.Parallel()

	auth := new(mockAuthClient)
	svc := service.NewDeviceAuthService(auth)
	cert := testCert(t, "device-002")

	auth.On("ValidateDevice", mock.Anything, "device-002", cert.SerialNumber.String()).
		Return(false, nil)

	_, err := svc.ValidateCertificate(context.Background(), cert)
	assert.EqualError(t, err, "device not authorized")
}

func TestDeviceAuthService_ValidateCertificate_AuthClientError(t *testing.T) {
	t.Parallel()

	auth := new(mockAuthClient)
	svc := service.NewDeviceAuthService(auth)
	cert := testCert(t, "device-003")

	auth.On("ValidateDevice", mock.Anything, "device-003", cert.SerialNumber.String()).
		Return(false, assert.AnError)

	_, err := svc.ValidateCertificate(context.Background(), cert)
	assert.ErrorIs(t, err, assert.AnError)
}
