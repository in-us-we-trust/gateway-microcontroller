package requestctx

import "context"

type DeviceContext struct {
	DeviceID string
	Serial   string
}

type deviceContextKey struct{}

const (
	DeviceIDHeader = "device_id"
)

func WithDeviceContext(ctx context.Context, device DeviceContext) context.Context {
	return context.WithValue(ctx, deviceContextKey{}, device)
}

func GetDeviceContext(ctx context.Context) (DeviceContext, bool) {
	device, ok := ctx.Value(deviceContextKey{}).(DeviceContext)
	return device, ok
}
