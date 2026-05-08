package pricing

import (
	"log/slog"
)

// regionPrefix maps an AWS regionCode to the prefix used in the Pricing
// API's usagetype values. Pattern observed: us-east-2 → "USE2",
// eu-west-1 → "EUW1", ap-northeast-1 → "APN1".
//
// AWS does not expose this mapping in any public API — it lives in the
// billing schema only — so it is hard-coded here. The list covers the
// regions most commonly used by the kind of teams that look at PR cost
// diffs; gov-cloud, China, and the more exotic regions are out of scope.
// Unknown regions emit a slog.Warn and fall back to returning the region
// string unchanged. That fallback is rarely catastrophic because most
// queries that need a usagetype filter also carry at least one other
// discriminator (engine, instance type), so the query still narrows
// correctly enough to surface a clear "no products found" error.
//
// Currently only EstimateLambda's Provisioned Concurrency lookup uses
// this — usagetype is the only filter that disambiguates the x86 and
// arm64 PC SKUs (AWS does not expose `architecture` as an attribute on
// those products).
func regionPrefix(region string) string {
	switch region {
	case "us-east-1":
		return "USE1"
	case "us-east-2":
		return "USE2"
	case "us-west-1":
		return "USW1"
	case "us-west-2":
		return "USW2"
	case "eu-west-1":
		return "EUW1"
	case "eu-central-1":
		return "EUC1"
	case "ap-southeast-1":
		return "APS1"
	case "ap-northeast-1":
		return "APN1"
	}
	slog.Warn("pricing: no usagetype prefix mapped for region; using region literal as fallback",
		"region", region,
	)
	return region
}
