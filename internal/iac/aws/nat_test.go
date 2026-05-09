package aws

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractNATGateway_HappyPath(t *testing.T) {
	attrs := map[string]interface{}{
		"subnet_id":         "subnet-0a1b2c3d4e5f",
		"connectivity_type": "private",
		"some_extra":        "ignored",
	}
	got, err := ExtractNATGateway(attrs)
	if err != nil {
		t.Fatalf("ExtractNATGateway: %v", err)
	}
	want := NATGatewayAttributes{
		SubnetID:         "subnet-0a1b2c3d4e5f",
		ConnectivityType: "private",
	}
	if *got != want {
		t.Errorf("got %+v\nwant %+v", *got, want)
	}
}

func TestExtractNATGateway_OnlyRequiredFields(t *testing.T) {
	got, err := ExtractNATGateway(map[string]interface{}{
		"subnet_id": "subnet-deadbeef",
	})
	if err != nil {
		t.Fatalf("ExtractNATGateway: %v", err)
	}
	if got.ConnectivityType != "public" {
		t.Errorf("ConnectivityType = %q, want public (default)", got.ConnectivityType)
	}
}

func TestExtractNATGateway_MissingSubnetID(t *testing.T) {
	_, err := ExtractNATGateway(map[string]interface{}{
		"connectivity_type": "public",
	})
	if err == nil {
		t.Fatal("expected error for missing subnet_id")
	}
	want := `aws_nat_gateway: missing required attribute "subnet_id"`
	if err.Error() != want {
		t.Errorf("error = %q\nwant %q", err.Error(), want)
	}
}

func TestExtractNATGateway_WrongTypes(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]interface{}
		want  string
	}{
		{"subnet_id as int", map[string]interface{}{"subnet_id": 42}, "subnet_id"},
		{"connectivity_type as bool", map[string]interface{}{
			"subnet_id":         "subnet-x",
			"connectivity_type": true,
		}, "connectivity_type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ExtractNATGateway(c.attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error should mention %q: %v", c.want, err)
			}
			if !strings.HasPrefix(err.Error(), "aws_nat_gateway") {
				t.Errorf("error should start with aws_nat_gateway: %v", err)
			}
		})
	}
}

func TestExtractNATGateway_NilAndEmpty(t *testing.T) {
	for _, attrs := range []map[string]interface{}{nil, {}} {
		_, err := ExtractNATGateway(attrs)
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != "aws_nat_gateway: empty attributes" {
			t.Errorf("unexpected message: %q", err.Error())
		}
	}
}

func ExampleExtractNATGateway() {
	attrs := map[string]interface{}{
		"subnet_id": "subnet-1a2b3c4d",
	}
	out, err := ExtractNATGateway(attrs)
	if err != nil {
		panic(err)
	}
	fmt.Printf("nat in %s (type=%s)\n", out.SubnetID, out.ConnectivityType)
	// Output: nat in subnet-1a2b3c4d (type=public)
}
