package rpc

import (
	"context"

	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
)

// ListDatasources lists all available datasources
func (h *Handler) ListDatasources(payload []byte) ([]byte, error) {
	ctx := context.Background()

	// Call AI Studio service API
	resp, err := ai_studio_sdk.ListDatasources(ctx, 1, 100, nil, "")
	if err != nil {
		return errorResponse("failed to list datasources")
	}

	return successResponse(map[string]interface{}{
		"datasources": resp.Datasources,
		"total_count": resp.TotalCount,
	})
}
