package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/stretchr/testify/require"
)

func TestProvideAdminHandlersWiresEntityHandler(t *testing.T) {
	entityHandler := &admin.EntityHandler{}

	handlers := ProvideAdminHandlers(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, entityHandler, nil, nil, nil, nil, nil, nil, nil, nil,
	)

	require.Same(t, entityHandler, handlers.Entity)
}
