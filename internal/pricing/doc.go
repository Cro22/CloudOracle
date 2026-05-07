// Package pricing wraps the AWS Pricing API for product lookups used by
// downstream cost-impact analysis.
//
// Scope: this package is a thin foundation client. It deliberately does
// NOT cache results, does NOT parse the per-product JSON returned by the
// API, and does NOT translate Terraform attribute structs into Pricing
// API filters. Those responsibilities live in later milestones (caching,
// per-service attribute mappers, monthly cost estimation).
//
// Quirks of the AWS Pricing API that this package handles for callers:
//
//  1. The Pricing API service endpoint is only available in us-east-1
//     and ap-south-1. NewClient hard-codes us-east-1 because it's the
//     default sane choice. The endpoint region is unrelated to the region
//     of the priced resource — that comes through as a regionCode filter
//     value in the GetProducts call.
//
//  2. Products come back as a []string of opaque JSON documents on the
//     PriceList field (each ~10–50 KB). We pass them through unparsed;
//     per-service mappers in a later milestone are responsible for
//     decoding them. Decoding here would couple this foundation to every
//     supported resource type.
//
//  3. Filters are list-of-(field, value) with a filter type. We force
//     TERM_MATCH for every filter — it's the only one we use today, and
//     ANDing exact matches is what every downstream caller wants.
//     ANY_OF / NONE_OF / CONTAINS are deliberately not exposed.
package pricing
