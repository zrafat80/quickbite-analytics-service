package rbac

import "context"

type PermissionSource interface {
	GetRolePermissions(ctx context.Context, role string) ([]string, error)
}
