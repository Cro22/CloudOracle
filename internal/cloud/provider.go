package cloud

import (
	"CloudOracle/internal/shared"
	"context"
)

type CloudProvider interface {
	Name() string
	FetchResources(ctx context.Context) ([]shared.Resource, error)
}
