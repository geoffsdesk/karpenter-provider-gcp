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

package v1alpha1

// ComputeClassRef is a reference to a GKE Custom Compute Class (cloud.google.com/v1 ComputeClass)
// that informs Karpenter's instance type selection with priority-based fallback rules.
// When specified, Karpenter reads the ComputeClass priority rules and uses them to
// filter and reorder instance type candidates during provisioning.
type ComputeClassRef struct {
	// Name is the name of the GKE ComputeClass resource to reference.
	// The ComputeClass must exist in the cluster. If it does not exist, Karpenter
	// will log a warning and fall back to standard instance type resolution.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`
	// +required
	Name string `json:"name"`
}

// ComputeClassPriorityRule represents a single priority rule extracted from a GKE ComputeClass.
// Rules are evaluated top-to-bottom; the first rule with available capacity is preferred.
type ComputeClassPriorityRule struct {
	// Priority is the index of this rule (0 = highest priority).
	Priority int `json:"priority"`

	// MachineType is an exact machine type match (e.g., "n4-standard-4").
	// If set, only this specific machine type matches.
	// +optional
	MachineType string `json:"machineType,omitempty"`

	// MachineFamily is a machine family prefix match (e.g., "n4", "c3").
	// If set, any machine type in this family matches.
	// +optional
	MachineFamily string `json:"machineFamily,omitempty"`

	// Spot indicates whether this rule prefers spot (preemptible) instances.
	// If nil, both spot and on-demand are accepted.
	// +optional
	Spot *bool `json:"spot,omitempty"`
}

// ComputeClassSpec represents the subset of GKE ComputeClass fields that Karpenter consumes.
// This is a read-only representation; Karpenter does not modify the ComputeClass resource.
type ComputeClassSpec struct {
	// Priorities is the ordered list of priority rules from the ComputeClass.
	Priorities []ComputeClassPriorityRule `json:"priorities,omitempty"`
}
