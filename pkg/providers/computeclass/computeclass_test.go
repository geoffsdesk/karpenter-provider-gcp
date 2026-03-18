/*
Copyright 2026 The CloudPilot AI Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package computeclass

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"

	"github.com/cloudpilot-ai/karpenter-provider-gcp/pkg/apis/v1alpha1"
)

// makeInstanceType creates a test instance type with the given name and offerings.
func makeInstanceType(name string, offerings ...*cloudprovider.Offering) *cloudprovider.InstanceType {
	return &cloudprovider.InstanceType{
		Name:      name,
		Offerings: offerings,
	}
}

// makeOffering creates a test offering with the given zone, capacity type, price, and availability.
func makeOffering(zone, capacityType string, price float64, available bool) *cloudprovider.Offering {
	return &cloudprovider.Offering{
		Requirements: scheduling.NewRequirements(
			scheduling.NewRequirement(karpv1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, capacityType),
			scheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, zone),
		),
		Price:     price,
		Available: available,
	}
}

func TestMatchesRule_MachineType(t *testing.T) {
	it := makeInstanceType("n4-standard-4",
		makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.10, true),
	)

	tests := []struct {
		name     string
		rule     v1alpha1.ComputeClassPriorityRule
		expected bool
	}{
		{
			name:     "exact machine type match",
			rule:     v1alpha1.ComputeClassPriorityRule{MachineType: "n4-standard-4"},
			expected: true,
		},
		{
			name:     "exact machine type mismatch",
			rule:     v1alpha1.ComputeClassPriorityRule{MachineType: "n4-standard-8"},
			expected: false,
		},
		{
			name:     "empty machine type matches all",
			rule:     v1alpha1.ComputeClassPriorityRule{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesRule(it, tt.rule)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesRule_MachineFamily(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		family       string
		expected     bool
	}{
		{
			name:         "n4 family matches n4-standard-4",
			instanceName: "n4-standard-4",
			family:       "n4",
			expected:     true,
		},
		{
			name:         "n2 family does not match n4-standard-4",
			instanceName: "n4-standard-4",
			family:       "n2",
			expected:     false,
		},
		{
			name:         "c3 family matches c3-standard-8",
			instanceName: "c3-standard-8",
			family:       "c3",
			expected:     true,
		},
		{
			name:         "e2 family matches e2-medium",
			instanceName: "e2-medium",
			family:       "e2",
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := makeInstanceType(tt.instanceName,
				makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.10, true),
			)
			rule := v1alpha1.ComputeClassPriorityRule{MachineFamily: tt.family}
			result := matchesRule(it, rule)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesRule_Spot(t *testing.T) {
	spotTrue := true
	spotFalse := false

	tests := []struct {
		name         string
		offerings    []*cloudprovider.Offering
		spot         *bool
		expected     bool
	}{
		{
			name: "spot rule matches instance with spot offering",
			offerings: []*cloudprovider.Offering{
				makeOffering("us-central1-a", karpv1.CapacityTypeSpot, 0.05, true),
			},
			spot:     &spotTrue,
			expected: true,
		},
		{
			name: "spot rule does not match instance with only on-demand",
			offerings: []*cloudprovider.Offering{
				makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.10, true),
			},
			spot:     &spotTrue,
			expected: false,
		},
		{
			name: "on-demand rule matches instance with on-demand offering",
			offerings: []*cloudprovider.Offering{
				makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.10, true),
			},
			spot:     &spotFalse,
			expected: true,
		},
		{
			name: "nil spot matches anything",
			offerings: []*cloudprovider.Offering{
				makeOffering("us-central1-a", karpv1.CapacityTypeSpot, 0.05, true),
			},
			spot:     nil,
			expected: true,
		},
		{
			name: "spot rule does not match unavailable spot offerings",
			offerings: []*cloudprovider.Offering{
				makeOffering("us-central1-a", karpv1.CapacityTypeSpot, 0.05, false), // unavailable
			},
			spot:     &spotTrue,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := makeInstanceType("n4-standard-4", tt.offerings...)
			rule := v1alpha1.ComputeClassPriorityRule{Spot: tt.spot}
			result := matchesRule(it, rule)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesRule_Combined(t *testing.T) {
	spotTrue := true

	it := makeInstanceType("n4-standard-4",
		makeOffering("us-central1-a", karpv1.CapacityTypeSpot, 0.05, true),
		makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.10, true),
	)

	tests := []struct {
		name     string
		rule     v1alpha1.ComputeClassPriorityRule
		expected bool
	}{
		{
			name: "family + spot matches",
			rule: v1alpha1.ComputeClassPriorityRule{
				MachineFamily: "n4",
				Spot:          &spotTrue,
			},
			expected: true,
		},
		{
			name: "wrong family + spot does not match",
			rule: v1alpha1.ComputeClassPriorityRule{
				MachineFamily: "c3",
				Spot:          &spotTrue,
			},
			expected: false,
		},
		{
			name: "exact type + spot matches",
			rule: v1alpha1.ComputeClassPriorityRule{
				MachineType: "n4-standard-4",
				Spot:        &spotTrue,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesRule(it, tt.rule)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterAndReorder_NilRules(t *testing.T) {
	provider := &DefaultProvider{}
	ctx := context.Background()

	candidates := []*cloudprovider.InstanceType{
		makeInstanceType("n4-standard-4", makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.10, true)),
		makeInstanceType("n4-standard-8", makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.20, true)),
	}

	result := provider.FilterAndReorder(ctx, candidates, nil)
	require.Len(t, result, 2)
	assert.Equal(t, "n4-standard-4", result[0].Name)
	assert.Equal(t, "n4-standard-8", result[1].Name)
}

func TestFilterAndReorder_EmptyCandidates(t *testing.T) {
	provider := &DefaultProvider{}
	ctx := context.Background()

	rules := []v1alpha1.ComputeClassPriorityRule{
		{Priority: 0, MachineFamily: "n4"},
	}

	result := provider.FilterAndReorder(ctx, nil, rules)
	assert.Empty(t, result)
}

func TestFilterAndReorder_PriorityOrdering(t *testing.T) {
	provider := &DefaultProvider{}
	ctx := context.Background()
	spotTrue := true

	candidates := []*cloudprovider.InstanceType{
		makeInstanceType("c3-standard-4",
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.15, true)),
		makeInstanceType("n4-standard-4",
			makeOffering("us-central1-a", karpv1.CapacityTypeSpot, 0.03, true),
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.10, true)),
		makeInstanceType("n2-standard-4",
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.12, true)),
	}

	rules := []v1alpha1.ComputeClassPriorityRule{
		{Priority: 0, MachineFamily: "n4", Spot: &spotTrue},         // Priority 0: spot n4
		{Priority: 1, MachineFamily: "n4"},                          // Priority 1: any n4
		{Priority: 2, MachineFamily: "n2"},                          // Priority 2: n2
	}

	result := provider.FilterAndReorder(ctx, candidates, rules)
	require.Len(t, result, 3)

	// n4-standard-4 should be first (matches priority 0: spot n4)
	assert.Equal(t, "n4-standard-4", result[0].Name)
	// n2-standard-4 should be second (matches priority 2: n2)
	assert.Equal(t, "n2-standard-4", result[1].Name)
	// c3-standard-4 should be last (matches no rule, fallback tier)
	assert.Equal(t, "c3-standard-4", result[2].Name)
}

func TestFilterAndReorder_PriceOrderingWithinTier(t *testing.T) {
	provider := &DefaultProvider{}
	ctx := context.Background()

	candidates := []*cloudprovider.InstanceType{
		makeInstanceType("n4-standard-8",
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.20, true)),
		makeInstanceType("n4-standard-2",
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.05, true)),
		makeInstanceType("n4-standard-4",
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.10, true)),
	}

	rules := []v1alpha1.ComputeClassPriorityRule{
		{Priority: 0, MachineFamily: "n4"}, // All match same priority
	}

	result := provider.FilterAndReorder(ctx, candidates, rules)
	require.Len(t, result, 3)

	// Within same priority tier, sorted by price ASC
	assert.Equal(t, "n4-standard-2", result[0].Name)
	assert.Equal(t, "n4-standard-4", result[1].Name)
	assert.Equal(t, "n4-standard-8", result[2].Name)
}

func TestFilterAndReorder_AllUnmatched(t *testing.T) {
	provider := &DefaultProvider{}
	ctx := context.Background()

	candidates := []*cloudprovider.InstanceType{
		makeInstanceType("c3-standard-4",
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.15, true)),
		makeInstanceType("e2-medium",
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.05, true)),
	}

	rules := []v1alpha1.ComputeClassPriorityRule{
		{Priority: 0, MachineFamily: "n4"}, // Nothing matches
	}

	result := provider.FilterAndReorder(ctx, candidates, rules)
	require.Len(t, result, 2)

	// Unmatched sorted by price
	assert.Equal(t, "e2-medium", result[0].Name)
	assert.Equal(t, "c3-standard-4", result[1].Name)
}

func TestFilterAndReorder_CostOptimizedFallback(t *testing.T) {
	// Simulates the full cost-optimized ComputeClass pattern:
	// Priority 0: Spot N4
	// Priority 1: Spot N2
	// Priority 2: On-demand N4
	// Fallback: everything else

	provider := &DefaultProvider{}
	ctx := context.Background()
	spotTrue := true
	spotFalse := false

	candidates := []*cloudprovider.InstanceType{
		makeInstanceType("c3-standard-4",
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.18, true)),
		makeInstanceType("n2-standard-4",
			makeOffering("us-central1-a", karpv1.CapacityTypeSpot, 0.04, true),
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.12, true)),
		makeInstanceType("n4-standard-4",
			makeOffering("us-central1-a", karpv1.CapacityTypeSpot, 0.03, true),
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.10, true)),
		makeInstanceType("n4-standard-8",
			makeOffering("us-central1-a", karpv1.CapacityTypeSpot, 0.06, true),
			makeOffering("us-central1-a", karpv1.CapacityTypeOnDemand, 0.20, true)),
	}

	rules := []v1alpha1.ComputeClassPriorityRule{
		{Priority: 0, MachineFamily: "n4", Spot: &spotTrue},      // Spot N4
		{Priority: 1, MachineFamily: "n2", Spot: &spotTrue},      // Spot N2
		{Priority: 2, MachineFamily: "n4", Spot: &spotFalse},     // On-demand N4
	}

	result := provider.FilterAndReorder(ctx, candidates, rules)
	require.Len(t, result, 4)

	names := lo.Map(result, func(it *cloudprovider.InstanceType, _ int) string { return it.Name })

	// Priority 0 (spot n4): n4-standard-4 ($0.03), n4-standard-8 ($0.06)
	// Priority 1 (spot n2): n2-standard-4 ($0.04)
	// Fallback: c3-standard-4 ($0.18)
	assert.Equal(t, []string{"n4-standard-4", "n4-standard-8", "n2-standard-4", "c3-standard-4"}, names)
}

func TestParsePriorityRules(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cloud.google.com/v1",
			"kind":       "ComputeClass",
			"metadata": map[string]interface{}{
				"name": "cost-optimized",
			},
			"spec": map[string]interface{}{
				"priorities": []interface{}{
					map[string]interface{}{
						"spot":          true,
						"machineFamily": "n4",
					},
					map[string]interface{}{
						"machineType": "n2-standard-4",
					},
					map[string]interface{}{
						"spot":          false,
						"machineFamily": "c3",
					},
				},
			},
		},
	}

	rules, err := parsePriorityRules(obj)
	require.NoError(t, err)
	require.Len(t, rules, 3)

	// Rule 0: spot n4
	assert.Equal(t, 0, rules[0].Priority)
	assert.Equal(t, "n4", rules[0].MachineFamily)
	assert.NotNil(t, rules[0].Spot)
	assert.True(t, *rules[0].Spot)

	// Rule 1: exact n2-standard-4
	assert.Equal(t, 1, rules[1].Priority)
	assert.Equal(t, "n2-standard-4", rules[1].MachineType)
	assert.Nil(t, rules[1].Spot)

	// Rule 2: on-demand c3
	assert.Equal(t, 2, rules[2].Priority)
	assert.Equal(t, "c3", rules[2].MachineFamily)
	assert.NotNil(t, rules[2].Spot)
	assert.False(t, *rules[2].Spot)
}

func TestParsePriorityRules_MissingSpec(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cloud.google.com/v1",
			"kind":       "ComputeClass",
			"metadata":   map[string]interface{}{"name": "test"},
		},
	}

	rules, err := parsePriorityRules(obj)
	assert.Error(t, err)
	assert.Nil(t, rules)
}

func TestParsePriorityRules_EmptyPriorities(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cloud.google.com/v1",
			"kind":       "ComputeClass",
			"metadata":   map[string]interface{}{"name": "test"},
			"spec": map[string]interface{}{
				"priorities": []interface{}{},
			},
		},
	}

	rules, err := parsePriorityRules(obj)
	require.NoError(t, err)
	assert.Empty(t, rules)
}
