package pricing

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture returns the contents of a JSON file under testdata/. Panics
// (via t.Fatal) on any IO error so the test fails loudly with a path the
// developer can investigate.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loading fixture %q: %v", path, err)
	}
	return string(raw)
}

func TestParseOnDemandPriceUSD_HappyPathCompute(t *testing.T) {
	body := loadFixture(t, "ec2_t3_large_us_east_2.json")
	price, unit, err := parseOnDemandPriceUSD(body)
	if err != nil {
		t.Fatalf("parseOnDemandPriceUSD: %v", err)
	}
	if math.Abs(price-0.0832) > 1e-9 {
		t.Errorf("price = %v, want 0.0832", price)
	}
	if unit != "Hrs" {
		t.Errorf("unit = %q, want Hrs", unit)
	}
}

func TestParseOnDemandPriceUSD_HappyPathStorage(t *testing.T) {
	body := loadFixture(t, "ec2_gp3_us_east_2.json")
	price, unit, err := parseOnDemandPriceUSD(body)
	if err != nil {
		t.Fatalf("parseOnDemandPriceUSD: %v", err)
	}
	if math.Abs(price-0.08) > 1e-9 {
		t.Errorf("price = %v, want 0.08", price)
	}
	if unit != "GB-Mo" {
		t.Errorf("unit = %q, want GB-Mo", unit)
	}
}

func TestParseOnDemandPriceUSD_MultipleSKUsPicksFirstSorted(t *testing.T) {
	// Two SKUs: "ZZZ.JRTC" (price 0.99) and "AAA.JRTC" (price 0.01).
	// Sorted ascending the AAA SKU wins regardless of map iteration order.
	body := `{
		"terms": {
			"OnDemand": {
				"ZZZ.JRTC": {
					"priceDimensions": {
						"ZZZ.JRTC.D1": {
							"unit": "Hrs",
							"pricePerUnit": {"USD": "0.99"}
						}
					}
				},
				"AAA.JRTC": {
					"priceDimensions": {
						"AAA.JRTC.D1": {
							"unit": "Hrs",
							"pricePerUnit": {"USD": "0.01"}
						}
					}
				}
			}
		}
	}`
	price, _, err := parseOnDemandPriceUSD(body)
	if err != nil {
		t.Fatalf("parseOnDemandPriceUSD: %v", err)
	}
	if math.Abs(price-0.01) > 1e-9 {
		t.Errorf("price = %v, want 0.01 (sorted-first SKU)", price)
	}
}

func TestParseOnDemandPriceUSD_MultiplePriceDimensionsPicksFirstSorted(t *testing.T) {
	body := `{
		"terms": {
			"OnDemand": {
				"S1.T1": {
					"priceDimensions": {
						"S1.T1.ZZZ": {"unit": "Hrs", "pricePerUnit": {"USD": "9.00"}},
						"S1.T1.AAA": {"unit": "Hrs", "pricePerUnit": {"USD": "1.00"}}
					}
				}
			}
		}
	}`
	price, _, err := parseOnDemandPriceUSD(body)
	if err != nil {
		t.Fatalf("parseOnDemandPriceUSD: %v", err)
	}
	if math.Abs(price-1.00) > 1e-9 {
		t.Errorf("price = %v, want 1.00 (sorted-first dimension)", price)
	}
}

func TestParseOnDemandPriceUSD_InvalidJSON(t *testing.T) {
	_, _, err := parseOnDemandPriceUSD("not-json{{{")
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
	if !strings.Contains(err.Error(), "parsing product JSON") {
		t.Errorf("error missing context: %v", err)
	}
}

func TestParseOnDemandPriceUSD_MissingTerms(t *testing.T) {
	_, _, err := parseOnDemandPriceUSD(`{"product": {}}`)
	if err == nil {
		t.Fatal("expected error when terms is missing")
	}
	if !strings.Contains(err.Error(), "no OnDemand pricing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseOnDemandPriceUSD_MissingOnDemand(t *testing.T) {
	body := `{"terms": {"Reserved": {"X.Y": {}}}}`
	_, _, err := parseOnDemandPriceUSD(body)
	if err == nil {
		t.Fatal("expected error when OnDemand block is missing")
	}
	if !strings.Contains(err.Error(), "no OnDemand pricing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseOnDemandPriceUSD_EmptyOnDemand(t *testing.T) {
	body := `{"terms": {"OnDemand": {}}}`
	_, _, err := parseOnDemandPriceUSD(body)
	if err == nil {
		t.Fatal("expected error on empty OnDemand block")
	}
	if !strings.Contains(err.Error(), "no OnDemand pricing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseOnDemandPriceUSD_MissingPriceDimensions(t *testing.T) {
	body := `{"terms": {"OnDemand": {"S.T": {}}}}`
	_, _, err := parseOnDemandPriceUSD(body)
	if err == nil {
		t.Fatal("expected error when priceDimensions missing")
	}
	if !strings.Contains(err.Error(), "priceDimensions") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseOnDemandPriceUSD_MissingUSD(t *testing.T) {
	body := `{
		"terms": {
			"OnDemand": {
				"S.T": {
					"priceDimensions": {
						"S.T.D": {"unit": "Hrs", "pricePerUnit": {"CNY": "0.10"}}
					}
				}
			}
		}
	}`
	_, _, err := parseOnDemandPriceUSD(body)
	if err == nil {
		t.Fatal("expected error when USD price missing")
	}
	if !strings.Contains(err.Error(), "no USD price") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseOnDemandPriceUSD_NonNumericPrice(t *testing.T) {
	body := `{
		"terms": {
			"OnDemand": {
				"S.T": {
					"priceDimensions": {
						"S.T.D": {"unit": "Hrs", "pricePerUnit": {"USD": "not-a-number"}}
					}
				}
			}
		}
	}`
	_, _, err := parseOnDemandPriceUSD(body)
	if err == nil {
		t.Fatal("expected error on non-numeric price")
	}
	if !strings.Contains(err.Error(), "parsing USD price") {
		t.Errorf("unexpected error: %v", err)
	}
}
