package rpc

import (
	"context"
	"fmt"

	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
)

// ListDatasources lists all available datasources
func (h *Handler) ListDatasources(payload []byte) ([]byte, error) {
	ctx := context.Background()

	// Call AI Studio service API
	resp, err := ai_studio_sdk.ListDatasources(ctx, 1, 100, nil, "")
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to list datasources: %v", err))
	}

	// Convert protobuf response to simple JSON
	datasources := make([]map[string]interface{}, len(resp.Datasources))
	for i, ds := range resp.Datasources {
		datasources[i] = map[string]interface{}{
			"id":                ds.Id,
			"name":              ds.Name,
			"short_description": ds.ShortDescription,
			"long_description":  ds.LongDescription,
			"db_source_type":    ds.DbSourceType,
			"active":            ds.Active,
		}
	}

	return successResponse(map[string]interface{}{
		"datasources": datasources,
		"total_count": resp.TotalCount,
	})
}
