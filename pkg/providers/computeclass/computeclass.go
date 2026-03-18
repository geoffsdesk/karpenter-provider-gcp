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

// Package computeclass implements ComputeClass-aware instance type filtering
// for the Karpenter GCP provider. It reads GKE ComputeClass CRDs and applies
// their priority rules as an additional filtering and ordering layer in
// Karpenter's instance type resolution pipeline.
package computeclass

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"

	"github.com/cloudpilot-ai/karpenter-provider-gcp/pkg/apis/v1alpha1"
)

var computeClassGVR = schema.GroupVersionResource{
	Group:    "cloud.google.com",
	Version:  "v1",
	Resource: "computeclasses",
}

// Provider resolves GKE ComputeClass resources and applies priority-based
// filtering to Karpenter's instance type candidates.
type Provider interface {
	// Resolve reads a ComputeClass by name and returns its parsed priority rules.
	// Returns nil rules and no error if the ComputeClass does not exist (graceful degradation).
	Resolve(ctx context.Context, ref *v1alpha1.ComputeClassRef) ([]v1alpha1.ComputeClassPriorityRule, error)

	// FilterAndReorder applies ComputeClass priority rules to a set of instance type
	// candidates. Returns candidates ordered by: priority ASC, then price ASC within tier.
	// If rules is nil or empty, returns the original candidates unchanged.
	FilterAndReorder(ctx context.Context, candidates []*cloudprovider.InstanceType, rules []v1alpha1.ComputeClassPriorityRule) []*cloudprovider.InstanceType
}

// DefaultProvider implements the ComputeClass provider using a Kubernetes client
// to read ComputeClass CRDs from the cluster.
type DefaultProvider struct {
	kubeClient client.Client
}

// NewDefaultProvider creates a new ComputeClass provider.
func NewDefaultProvider(kubeClient client.Client) *DefaultProvider {
	return &DefaultProvider{
		kubeClient: kubeClient,
	}
}

// Resolve reads a GKE ComputeClass by name using an unstructured client (since the
// ComputeClass CRD is owned by GKE, not by this provider). Returns parsed priority rules.
func (p *DefaultProvider) Resolve(ctx context.Context, ref *v1alpha1.ComputeClassRef) ([]v1alpha1.ComputeClassPriorityRule, error) {
	if ref == nil {
		return nil, nil
	}

	logger := log.FromContext(ctx).WithValues("computeClass", ref.Name)

	// Use unstructured client to read the GKE-owned ComputeClass CRD
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   computeClassGVR.Group,
		Version: computeClassGVR.Version,
		Kind:    "ComputeClass",
	})

	if err := p.kubeClient.Get(ctx, client.ObjectKey{Name: ref.Name}, obj); err != nil {
		logger.Info("ComputeClass not found, falling back to standard resolution", "error", err)
		return nil, nil // Graceful degradation: return nil rules, no error
	}

	rules, err := parsePriorityRules(obj)
	if err != nil {
		logger.Error(err, "failed to parse ComputeClass priority rules, falling back to standard resolution")
		return nil, nil // Graceful degradation
	}

	logger.Info("resolved ComputeClass", "ruleCount", len(rules))
	return rules, nil
}

// parsePriorityRules extracts priority rules from an unstructured ComputeClass object.
func parsePriorityRules(obj *unstructured.Unstructured) ([]v1alpha1.ComputeClassPriorityRule, error) {
	spec, ok, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil || !ok {
		return nil, fmt.Errorf("ComputeClass missing spec: %w", err)
	}

	prioritiesRaw, ok, err := unstructured.NestedSlice(spec, "priorities")
	if err != nil || !ok {
		return nil, fmt.Errorf("ComputeClass missing spec.priorities: %w", err)
	}

	var rules []v1alpha1.ComputeClassPriorityRule
	for i, raw := range prioritiesRaw {
		ruleMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		rule := v1alpha1.ComputeClassPriorityRule{
			Priority: i,
		}

		if mt, ok := ruleMap["machineType"].(string); ok {
			rule.MachineType = mt
		}
		if mf, ok := ruleMap["machineFamily"].(string); ok {
			rule.MachineFamily = mf
		}
		if spot, ok := ruleMap["spot"].(bool); ok {
			rule.Spot = &spot
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

// FilterAndReorder applies ComputeClass priority rules to instance type candidates.
//
// Algorithm:
//  1. For each candidate, find the highest-priority (lowest index) rule that matches
//  2. Tag each candidate with its matched priority level
//  3. Sort candidates by: priority ASC, then by cheapest available offering price ASC
//  4. Candidates that match no rule are placed last (acting as a fallback tier)
//
// This preserves Karpenter's cost-optimization within each priority tier while
// respecting the platform team's priority ordering across tiers.
func (p *DefaultProvider) FilterAndReorder(ctx context.Context, candidates []*cloudprovider.InstanceType, rules []v1alpha1.ComputeClassPriorityRule) []*cloudprovider.InstanceType {
	if len(rules) == 0 || len(candidates) == 0 {
		return candidates
	}

	logger := log.FromContext(ctx)

	type taggedCandidate struct {
		instanceType *cloudprovider.InstanceType
		priority     int     // matched priority index, or len(rules) for unmatched
		minPrice     float64 // cheapest available offering price
	}

	tagged := make([]taggedCandidate, 0, len(candidates))
	matchCounts := make(map[int]int) // priority -> count of matched candidates

	for _, candidate := range candidates {
		matchedPriority := len(rules) // default: unmatched (lowest priority)

		for _, rule := range rules {
			if matchesRule(candidate, rule) {
				matchedPriority = rule.Priority
				matchCounts[rule.Priority]++
				break // Use highest (first) matching priority
			}
		}

		// Find cheapest available offering price for sorting within tier
		minPrice := float64(1e18)
		for _, offering := range candidate.Offerings.Available() {
			if offering.Price < minPrice {
				minPrice = offering.Price
			}
		}

		tagged = append(tagged, taggedCandidate{
			instanceType: candidate,
			priority:     matchedPriority,
			minPrice:     minPrice,
		})
	}

	// Sort: priority ASC, then price ASC within same priority
	sort.SliceStable(tagged, func(i, j int) bool {
		if tagged[i].priority != tagged[j].priority {
			return tagged[i].priority < tagged[j].priority
		}
		return tagged[i].minPrice < tagged[j].minPrice
	})

	result := make([]*cloudprovider.InstanceType, len(tagged))
	for i, t := range tagged {
		result[i] = t.instanceType
	}

	// Log decision summary
	for priority, count := range matchCounts {
		logger.V(1).Info("ComputeClass priority match",
			"priority", priority,
			"matchedCandidates", count,
		)
	}
	unmatchedCount := 0
	for _, t := range tagged {
		if t.priority == len(rules) {
			unmatchedCount++
		}
	}
	if unmatchedCount > 0 {
		logger.V(1).Info("ComputeClass unmatched candidates placed in fallback tier",
			"count", unmatchedCount,
		)
	}

	return result
}

// matchesRule checks whether an instance type matches a ComputeClass priority rule.
func matchesRule(instanceType *cloudprovider.InstanceType, rule v1alpha1.ComputeClassPriorityRule) bool {
	name := instanceType.Name

	// Check machineType exact match
	if rule.MachineType != "" {
		if name != rule.MachineType {
			return false
		}
	}

	// Check machineFamily prefix match (e.g., "n4" matches "n4-standard-4")
	if rule.MachineFamily != "" {
		parts := strings.Split(name, "-")
		if len(parts) < 2 || parts[0] != rule.MachineFamily {
			return false
		}
	}

	// Check spot/on-demand capacity type match
	if rule.Spot != nil {
		wantCapacityType := karpv1.CapacityTypeOnDemand
		if *rule.Spot {
			wantCapacityType = karpv1.CapacityTypeSpot
		}

		// Check if any available offering matches the desired capacity type
		hasMatchingOffering := false
		for _, offering := range instanceType.Offerings.Available() {
			ct := offering.Requirements.Get(karpv1.CapacityTypeLabelKey).Any()
			if ct == wantCapacityType {
				hasMatchingOffering = true
				break
			}
		}
		if !hasMatchingOffering {
			return false
		}
	}

	return true
}
