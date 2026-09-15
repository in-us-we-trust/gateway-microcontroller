package requestctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceIDRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := WithDeviceID(context.Background(), "device-1")
	got, ok := DeviceID(ctx)
	require.True(t, ok)
	assert.Equal(t, "device-1", got)

	_, ok = DeviceID(context.Background())
	assert.False(t, ok)
}
