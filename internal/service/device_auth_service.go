package service

import (
	"context"
	"crypto/x509"
	"errors"
	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/domain"
)

type DeviceAuthService struct {
	authClient client.AuthClient
}

func NewDeviceAuthService(
	authClient client.AuthClient,
) *DeviceAuthService {
	return &DeviceAuthService{
		authClient: authClient,
	}
}

func (s *DeviceAuthService) ValidateCertificate(
	ctx context.Context,
	cert *x509.Certificate,
) (domain.AuthenticatedDevice, error) {

	deviceID, err := extractDeviceID(cert)
	if err != nil {
		return domain.AuthenticatedDevice{}, err
	}

	serial := cert.SerialNumber.String()

	valid, err := s.authClient.ValidateDevice(
		ctx,
		deviceID,
		serial,
	)

	if err != nil {
		return domain.AuthenticatedDevice{}, err
	}

	if !valid {
		return domain.AuthenticatedDevice{},
			errors.New("device not authorized")
	}

	return domain.AuthenticatedDevice{
		DeviceID: deviceID,
		Serial:   serial,
	}, nil
}

func extractDeviceID(
	cert *x509.Certificate,
) (string, error) {

	if cert.Subject.CommonName == "" {
		return "", errors.New(
			"device id not found in certificate",
		)
	}

	return cert.Subject.CommonName, nil
}
