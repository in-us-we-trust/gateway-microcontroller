package util

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIs2xx(t *testing.T) {
	t.Parallel()
	assert.True(t, Is2xx(http.StatusOK))
	assert.True(t, Is2xx(http.StatusCreated))
	assert.False(t, Is2xx(http.StatusBadRequest))
	assert.False(t, Is2xx(http.StatusBadGateway))
}
