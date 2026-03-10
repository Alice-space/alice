package domain

type ReceptionInput struct {
	Event         ExternalEvent `json:"event"`
	RouteTarget   RouteTarget   `json:"route_target"`
	RequestID     string        `json:"request_id"`
	RequiredRefs  []string      `json:"required_refs"`
	RouteSnapshot RouteSnapshot `json:"route_snapshot"`
}
