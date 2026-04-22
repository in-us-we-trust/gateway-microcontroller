package requestctx

import "context"

type ctxKey string

const (
	deviceIDKey    ctxKey = "device_id"
	DeviceIDHeader        = "device_id"
)

func WithDeviceID(ctx context.Context, deviceID string) context.Context {
	return context.WithValue(ctx, deviceIDKey, deviceID)
}

func DeviceID(ctx context.Context) (string, bool) {
	deviceID, ok := ctx.Value(deviceIDKey).(string)
	if !ok || deviceID == "" {
		return "", false
	}

	return deviceID, true
}
