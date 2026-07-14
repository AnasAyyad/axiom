package exchangecontracts

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDescriptorSerializationAndConstraints(t *testing.T) {
	t.Parallel()
	descriptor := Descriptor{
		Exchange: "fixture", Environment: EnvironmentProductionPublic, AccountMode: AccountModePublicOnly,
		Version: "v1", ObservedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		Capabilities: []Capability{
			{Feature: FeatureBookSnapshots, Support: Supported, Constraints: []Constraint{{Name: "depth", Values: []string{"100", "500"}}}},
			{Feature: FeatureOrders, Support: Unsupported},
		},
	}
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Descriptor
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Validate() != nil {
		t.Fatalf("descriptor round trip failed: %v", err)
	}
}

func TestDescriptorRejectsUnsafeShapes(t *testing.T) {
	t.Parallel()
	observed := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	cases := []Descriptor{
		{Exchange: "fixture", Environment: EnvironmentProductionPublic, AccountMode: AccountModePublicOnly, Version: "v1", ObservedAt: observed,
			Capabilities: []Capability{{Feature: FeatureOrders, Support: Unsupported, Constraints: []Constraint{{Name: "unexpected", Values: []string{"value"}}}}}},
		{Exchange: "fixture", Environment: EnvironmentProductionPublic, AccountMode: AccountModePublicOnly, Version: "v1", ObservedAt: observed,
			Capabilities: []Capability{{Feature: FeatureOrders, Support: Unsupported}, {Feature: FeatureOrders, Support: Unsupported}}},
		{Exchange: "fixture", Environment: "unknown", AccountMode: AccountModePublicOnly, Version: "v1", ObservedAt: observed},
	}
	for _, descriptor := range cases {
		if err := descriptor.Validate(); KindOf(err) != ErrorValidation {
			t.Fatalf("unsafe descriptor accepted: %+v err=%v", descriptor, err)
		}
	}
}
