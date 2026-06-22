package coreclient

// EnvelopeSuccess is the shape core-service returns on 2xx. We only care
// about the `data` field — discriminating on `success` is enough.
type EnvelopeSuccess struct {
	IsSuccess  bool `json:"isSuccess"`
	StatusCode int  `json:"statusCode"`
	Data       any  `json:"data"`
}

// RolePermissionsResponse mirrors core's GET /api/roles/:role/permissions
// success body. Core flattens the (resource, action) rows on its end and
// returns a single string array per docs/business-logic/rbac.md, so we read
// it directly rather than recomposing here.
type RolePermissionsResponse struct {
	IsSuccess  bool                `json:"isSuccess"`
	StatusCode int                 `json:"statusCode"`
	Data       RolePermissionsData `json:"data"`
}

type RolePermissionsData struct {
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}
