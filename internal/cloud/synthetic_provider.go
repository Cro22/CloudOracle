package cloud

import (
	"CloudOracle/internal/generator"
	"CloudOracle/internal/shared"
	"context"
)

type SyntheticProvider struct {
	count     int
	accountID string
}

func NewSyntheticProvider(count int, accountID string) *SyntheticProvider {
	return &SyntheticProvider{
		count:     count,
		accountID: accountID,
	}
}

func (s *SyntheticProvider) Name() string {
	return "synthetic"
}

func (s *SyntheticProvider) FetchResources(_ context.Context) ([]shared.Resource, error) {
	return generator.GenerateResources(s.count, s.accountID), nil
}
