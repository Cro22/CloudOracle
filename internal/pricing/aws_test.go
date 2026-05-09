package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

// fakePricing records every GetProducts call and returns the next
// pre-canned response. Designed for table-style tests: pages are
// consumed in order; if the test runs past the end of pages, the fake
// errors loudly so a missing setup is obvious.
type fakePricing struct {
	pages []fakePage
	calls []pricing.GetProductsInput

	// loop, when set, makes the fake return pages[0] forever instead
	// of advancing. Used to exercise the pagination cap.
	loop bool

	// onCall is invoked with the (1-indexed) call number before the
	// response is returned. Used to inject side effects like cancelling
	// the caller's context between pages.
	onCall func(callNum int)

	// firstCallErr, when non-nil, is returned on the first call instead
	// of pages[0]. Subsequent calls behave as configured.
	firstCallErr error
}

type fakePage struct {
	priceList []string
	nextToken string // empty means terminal page
}

func (f *fakePricing) GetProducts(_ context.Context, params *pricing.GetProductsInput, _ ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
	// Defensive copy: callers may reuse the input struct across calls.
	f.calls = append(f.calls, *params)

	if f.onCall != nil {
		f.onCall(len(f.calls))
	}

	if len(f.calls) == 1 && f.firstCallErr != nil {
		return nil, f.firstCallErr
	}

	idx := len(f.calls) - 1
	if f.loop {
		idx = 0
	}
	if idx >= len(f.pages) {
		return nil, fmt.Errorf("fakePricing: unexpected call #%d (only %d pages configured)", len(f.calls), len(f.pages))
	}

	p := f.pages[idx]
	out := &pricing.GetProductsOutput{PriceList: p.priceList}
	if p.nextToken != "" {
		tok := p.nextToken
		out.NextToken = &tok
	}
	return out, nil
}

func TestGetProducts_Success(t *testing.T) {
	want := []string{`{"product":"a"}`, `{"product":"b"}`, `{"product":"c"}`}
	fake := &fakePricing{
		pages: []fakePage{{priceList: want}},
	}
	c := newClientWithAPI(fake)

	got, err := c.GetProducts(context.Background(), "AmazonEC2", map[string]string{
		"instanceType": "t3.large",
	})
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d products, want %d", len(got), len(want))
	}
	for i, p := range got {
		if p != want[i] {
			t.Errorf("product[%d] = %q, want %q", i, p, want[i])
		}
	}
	if len(fake.calls) != 1 {
		t.Errorf("expected exactly 1 API call, got %d", len(fake.calls))
	}
	if fake.calls[0].ServiceCode == nil || *fake.calls[0].ServiceCode != "AmazonEC2" {
		t.Errorf("ServiceCode not propagated: %+v", fake.calls[0].ServiceCode)
	}
}

func TestGetProducts_Pagination(t *testing.T) {
	fake := &fakePricing{
		pages: []fakePage{
			{priceList: []string{"p1", "p2"}, nextToken: "tok-page-2"},
			{priceList: []string{"p3"}},
		},
	}
	c := newClientWithAPI(fake)

	got, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	want := []string{"p1", "p2", "p3"}
	if len(got) != len(want) {
		t.Fatalf("got %d products, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("product[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if len(fake.calls) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(fake.calls))
	}
	// First call: no NextToken.
	if fake.calls[0].NextToken != nil {
		t.Errorf("first call NextToken = %q, want nil", *fake.calls[0].NextToken)
	}
	// Second call: NextToken from the first response.
	if fake.calls[1].NextToken == nil || *fake.calls[1].NextToken != "tok-page-2" {
		t.Errorf("second call NextToken = %v, want %q", fake.calls[1].NextToken, "tok-page-2")
	}
}

func TestGetProducts_FiltersMappedCorrectly(t *testing.T) {
	fake := &fakePricing{pages: []fakePage{{priceList: nil}}}
	c := newClientWithAPI(fake)

	in := map[string]string{
		"instanceType":    "t3.large",
		"regionCode":      "us-east-2",
		"operatingSystem": "Linux",
	}
	if _, err := c.GetProducts(context.Background(), "AmazonEC2", in); err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fake.calls))
	}
	got := fake.calls[0].Filters
	if len(got) != len(in) {
		t.Fatalf("got %d filters, want %d", len(got), len(in))
	}
	for _, f := range got {
		if f.Type != types.FilterTypeTermMatch {
			t.Errorf("filter type = %q, want TERM_MATCH", f.Type)
		}
		if f.Field == nil || f.Value == nil {
			t.Fatalf("filter has nil Field/Value: %+v", f)
		}
		want, ok := in[*f.Field]
		if !ok {
			t.Errorf("unexpected filter field %q", *f.Field)
			continue
		}
		if *f.Value != want {
			t.Errorf("filter %q = %q, want %q", *f.Field, *f.Value, want)
		}
	}
}

func TestGetProducts_EmptyServiceCode(t *testing.T) {
	fake := &fakePricing{}
	c := newClientWithAPI(fake)

	_, err := c.GetProducts(context.Background(), "", nil)
	if err == nil {
		t.Fatal("expected error for empty serviceCode")
	}
	if err.Error() != "pricing: empty serviceCode" {
		t.Errorf("error = %q, want %q", err.Error(), "pricing: empty serviceCode")
	}
	if len(fake.calls) != 0 {
		t.Errorf("API was called %d times despite empty serviceCode", len(fake.calls))
	}
}

func TestGetProducts_APIError(t *testing.T) {
	apiErr := errors.New("AccessDenied: not authorized")
	fake := &fakePricing{firstCallErr: apiErr}
	c := newClientWithAPI(fake)

	_, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, apiErr) {
		t.Errorf("error does not wrap underlying API error: %v", err)
	}
	if !strings.Contains(err.Error(), "pricing: GetProducts(AmazonEC2):") {
		t.Errorf("error missing wrap prefix: %q", err.Error())
	}
}

func TestGetProducts_PaginationCap(t *testing.T) {
	// Fake returns NextToken on every call — without the cap, the
	// loop would run forever. With the cap, it should stop after
	// maxPages and return what was collected.
	fake := &fakePricing{
		pages: []fakePage{{priceList: []string{"p"}, nextToken: "always"}},
		loop:  true,
	}
	c := newClientWithAPI(fake)

	got, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if len(fake.calls) != maxPages {
		t.Errorf("call count = %d, want %d (pagination cap)", len(fake.calls), maxPages)
	}
	if len(got) != maxPages {
		t.Errorf("product count = %d, want %d (one product per page)", len(got), maxPages)
	}
}

func TestGetProducts_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakePricing{
		pages: []fakePage{
			{priceList: []string{"p1"}, nextToken: "tok-2"},
			{priceList: []string{"p2"}, nextToken: "tok-3"},
			{priceList: []string{"p3"}},
		},
		// Cancel the parent context after the first successful page so
		// the next iteration's ctx.Err() check trips.
		onCall: func(n int) {
			if n == 1 {
				cancel()
			}
		},
	}
	c := newClientWithAPI(fake)

	_, err := c.GetProducts(ctx, "AmazonEC2", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error does not wrap context.Canceled: %v", err)
	}
	if !strings.Contains(err.Error(), "pricing: GetProducts(AmazonEC2):") {
		t.Errorf("error missing wrap prefix: %q", err.Error())
	}
	// Page 1 succeeded; the cancellation should have prevented page 2.
	if len(fake.calls) != 1 {
		t.Errorf("expected exactly 1 call before cancellation, got %d", len(fake.calls))
	}
}

func TestGetProducts_EmptyFilters(t *testing.T) {
	// Empty/nil filters must be allowed (queries every product for the
	// service). Verify the call goes through with zero filters.
	fake := &fakePricing{pages: []fakePage{{priceList: []string{"x"}}}}
	c := newClientWithAPI(fake)

	got, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d products, want 1", len(got))
	}
	if len(fake.calls[0].Filters) != 0 {
		t.Errorf("expected 0 filters, got %d", len(fake.calls[0].Filters))
	}
}

func TestNewClient_RegionForced(t *testing.T) {
	// LoadDefaultConfig touches the local AWS config files / env vars.
	// On a clean CI box without any AWS env, it still succeeds (config
	// loading is independent from credential resolution). If for some
	// reason the environment makes it fail, skip rather than fail —
	// the property under test is the region, not the credentials chain.
	c, err := NewClient(context.Background())
	if err != nil {
		t.Skipf("LoadDefaultConfig failed in this environment: %v", err)
	}
	pc, ok := c.api.(*pricing.Client)
	if !ok {
		t.Fatalf("api is %T, want *pricing.Client", c.api)
	}
	if got := pc.Options().Region; got != pricingRegion {
		t.Errorf("client Region = %q, want %q", got, pricingRegion)
	}
}
