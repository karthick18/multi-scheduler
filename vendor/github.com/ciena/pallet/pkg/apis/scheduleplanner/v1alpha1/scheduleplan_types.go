/*
Copyright 2022 Ciena Corporation.

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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SchedulePlanSpec defines a plan for a pod-set containing the pod to node assignments.
type SchedulePlanSpec struct {
	// +kubebuilder:validation:Required
	Plan []PlanSpec `json:"plan"`
}

// SchedulePlanStatus defines the plan status for a pod-set.
type SchedulePlanStatus struct{}

// PlanSpec defines a plan spec for pod to node assignments for a pod-set.
type PlanSpec struct {
	// +kubebuilder:validation:Required
	Pod string `json:"pod"`

	// +kubebuilder:validation:Required
	Node string `json:"node"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:servedversion
// +kubebuilder:resource:shortName=sp,scope=Namespaced,singular=scheduleplan
// +kubebuilder:printcolumn:name="Plan",type="string",JSONPath=".spec.plan",priority=1

// SchedulePlan is the Schema for the schedulePlan api
// +genclient.
type SchedulePlan struct {
	//nolint: tagliatelle
	metav1.TypeMeta `json:",inline"`

	//nolint: tagliatelle
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec SchedulePlanSpec `json:"spec"`

	// +optional
	Status SchedulePlanStatus `json:"status"`
}

// +kubebuilder:object:root=true

// SchedulePlanList contains a list of schedule plans.
type SchedulePlanList struct {
	//nolint: tagliatelle
	metav1.TypeMeta `json:",inline"`
	//nolint: tagliatelle
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SchedulePlan `json:"items"`
}

//nolint: gochecknoinits
func init() {
	SchemeBuilder.Register(&SchedulePlan{}, &SchedulePlanList{})
}
