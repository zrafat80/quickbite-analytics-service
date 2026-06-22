// Package coreclient — sync HTTP client to core-service. Mirrors
// order-service/src/lib/core-client. Only the RBAC permission endpoint is
// needed in this milestone; expand as the homework adds more.
package coreclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/zrafat80/quickbite/analytics-service/pkg/httpclient"
)

type Config struct {
	BaseURL        string
	InternalAPIKey string
}

type Client struct {
	cfg  Config
	http *httpclient.Client
}

func New(cfg Config, http *httpclient.Client) *Client {
	return &Client{cfg: cfg, http: http}
}

// GetRolePermissions fetches the permission list for a role. Returns the
// dotted strings (e.g. "orders:create") that callers will store in the RBAC
// cache. 404 from core => no permissions for that role (caller may surface
// a 403 to the user).
func (c *Client) GetRolePermissions(ctx context.Context, role string) ([]string, error) {
	url := fmt.Sprintf("%s/api/roles/%s/permissions", c.cfg.BaseURL, role)
	var resp RolePermissionsResponse
	status, err := c.http.DoJSON(ctx, http.MethodGet, url, map[string]string{
		"x-api-key": c.cfg.InternalAPIKey,
	}, nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("core get-role-permissions: %w", err)
	}
	if status == http.StatusNotFound {
		return []string{}, nil
	}
	if status >= 400 {
		return nil, fmt.Errorf("core get-role-permissions: status=%d", status)
	}
	// Core already flattens to "resource:action" strings on its end; pass them
	// through unchanged. Empty entries are dropped defensively.
	out := make([]string, 0, len(resp.Data.Permissions))
	for _, p := range resp.Data.Permissions {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
