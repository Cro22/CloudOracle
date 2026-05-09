package diff

import (
	"testing"

	"CloudOracle/internal/pricing"
)

func TestWeakestConfidence(t *testing.T) {
	cases := []struct {
		a, b, want pricing.Confidence
	}{
		{pricing.ConfidenceHigh, pricing.ConfidenceHigh, pricing.ConfidenceHigh},
		{pricing.ConfidenceHigh, pricing.ConfidenceMedium, pricing.ConfidenceMedium},
		{pricing.ConfidenceMedium, pricing.ConfidenceHigh, pricing.ConfidenceMedium},
		{pricing.ConfidenceHigh, pricing.ConfidenceLow, pricing.ConfidenceLow},
		{pricing.ConfidenceLow, pricing.ConfidenceHigh, pricing.ConfidenceLow},
		{pricing.ConfidenceMedium, pricing.ConfidenceMedium, pricing.ConfidenceMedium},
		{pricing.ConfidenceMedium, pricing.ConfidenceLow, pricing.ConfidenceLow},
		{pricing.ConfidenceLow, pricing.ConfidenceMedium, pricing.ConfidenceLow},
		{pricing.ConfidenceLow, pricing.ConfidenceLow, pricing.ConfidenceLow},
	}
	for _, c := range cases {
		if got := weakestConfidence(c.a, c.b); got != c.want {
			t.Errorf("weakestConfidence(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
