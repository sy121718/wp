package presentationcontract

import "context"

type InstanceRetirer interface {
	RetireInstance(ctx context.Context, projectID, instanceID string) error
}
