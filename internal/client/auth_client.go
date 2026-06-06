package client

import "context"

type AuthClient interface {
	ValidateDevice(
		ctx context.Context,
		deviceID string,
		serial string,
	) (bool, error)
}
