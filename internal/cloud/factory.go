package cloud

import (
	"context"
	"fmt"
	"os"
)

const ProviderEnvVar = "CLOUDORACLE_PROVIDER"

func NewProvider(ctx context.Context) (CloudProvider, error) {
	name := os.Getenv(ProviderEnvVar)

	switch name {
	case "aws":
		return NewAWSProvider(ctx)

	case "synthetic", "":
		return NewSyntheticProvider(100, "synthetic-account"), nil

	default:
		return nil, fmt.Errorf(
			"unknown provider %q (set %s to \"aws\" or \"synthetic\")",
			name, ProviderEnvVar,
		)
	}
}
