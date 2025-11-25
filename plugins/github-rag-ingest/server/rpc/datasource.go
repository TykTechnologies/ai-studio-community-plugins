package rpc

import (
	"context"
	"encoding/json"
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
			"db_name":           ds.DbName,
			"active":            ds.Active,
		}
	}

	return successResponse(map[string]interface{}{
		"datasources": datasources,
		"total_count": resp.TotalCount,
	})
}

// CloneDatasource clones a datasource via AI Studio service
func (h *Handler) CloneDatasource(payload []byte) ([]byte, error) {
	var req struct {
		SourceDatasourceID int `json:"source_datasource_id"`
	}

	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse(fmt.Sprintf("invalid request: %v", err))
	}

	if req.SourceDatasourceID == 0 {
		return errorResponse("source_datasource_id is required")
	}

	ctx := context.Background()

	// Call AI Studio SDK to clone datasource (server-side, preserves API keys)
	resp, err := ai_studio_sdk.CloneDatasource(ctx, uint32(req.SourceDatasourceID))
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to clone datasource: %v", err))
	}

	return successResponse(map[string]interface{}{
		"datasource_id": resp.Datasource.Id,
	})
}

// UpdateDatasourceFields updates name and namespace on a datasource
// Note: Empty strings for connection fields mean "preserve existing value" server-side
func (h *Handler) UpdateDatasourceFields(payload []byte) ([]byte, error) {
	var req struct {
		DatasourceID int    `json:"datasource_id"`
		Name         string `json:"name"`
		DBName       string `json:"db_name"`
	}

	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse(fmt.Sprintf("invalid request: %v", err))
	}

	if req.DatasourceID == 0 {
		return errorResponse("datasource_id is required")
	}

	if req.Name == "" {
		return errorResponse("name is required")
	}

	if req.DBName == "" {
		return errorResponse("db_name is required")
	}

	ctx := context.Background()

	// Get current datasource for metadata that proto exposes
	getResp, err := ai_studio_sdk.GetDatasource(ctx, uint32(req.DatasourceID))
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to get datasource: %v", err))
	}

	ds := getResp.Datasource

	// Extract tag names
	tagNames := make([]string, len(ds.Tags))
	for i, tag := range ds.Tags {
		tagNames[i] = tag.Name
	}

	// Update with new name and db_name, empty strings preserve existing connection config
	_, err = ai_studio_sdk.UpdateDatasourceWithEmbedder(
		ctx,
		uint32(req.DatasourceID),
		req.Name, // New name
		ds.ShortDescription,
		ds.LongDescription,
		ds.Url,
		"",              // Empty = preserve existing db_conn_string
		ds.DbSourceType,
		"",              // Empty = preserve existing db_conn_api_key
		req.DBName,      // New namespace
		ds.EmbedVendor,
		"",              // Empty = preserve existing embed_url
		"",              // Empty = preserve existing embed_api_key
		ds.EmbedModel,
		ds.PrivacyScore,
		ds.UserId,
		ds.Active,
		tagNames,
	)

	if err != nil {
		return errorResponse(fmt.Sprintf("failed to update datasource: %v", err))
	}

	return successResponse(map[string]interface{}{
		"message": "Datasource updated successfully",
	})
}
