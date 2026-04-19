package cloud

import (
	"CloudOracle/internal/config"
	"context"
	"fmt"
)

func NewProvider(ctx context.Context, cfg config.Config) (CloudProvider, error) {
	switch cfg.Cloud.Provider {
	case "aws":
		return NewAWSProvider(ctx, cfg)

	case "gcp":
		return NewGCPProvider(ctx, cfg)

	case "azure":
		return NewAzureProvider(ctx, cfg)

	case "synthetic", "":
		return NewSyntheticProvider(cfg.Cloud.SyntheticCount, cfg.Cloud.SyntheticAcct), nil

	default:
		return nil, fmt.Errorf(
			"unknown provider %q (set CLOUDORACLE_PROVIDER to \"aws\", \"gcp\", \"azure\", or \"synthetic\")",
			cfg.Cloud.Provider,
		)
	}
}
