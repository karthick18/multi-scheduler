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

// ScheduleTriggerSpec defines an event indicating that the pod-set can be transitioned from planning to schedule state.
type ScheduleTriggerSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Planning;Schedule
	State string `json:"state"`
}

// ScheduleTriggerStatus defines the status for a trigger.
type ScheduleTriggerStatus struct{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:servedversion
// +kubebuilder:resource:shortName=st,scope=Namespaced,singular=scheduletrigger
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".spec.state",priority=1

// ScheduleTrigger is the Schema for the scheduleTrigger api
// +genclient.
type ScheduleTrigger struct {
	//nolint: tagliatelle
	metav1.TypeMeta `json:",inline"`

	//nolint: tagliatelle
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec ScheduleTriggerSpec `json:"spec"`

	// +optional
	Status ScheduleTriggerStatus `json:"status"`
}

// +kubebuilder:object:root=true

// ScheduleTriggerList contains a list of schedule triggers.
type ScheduleTriggerList struct {
	//nolint: tagliatelle
	metav1.TypeMeta `json:",inline"`
	//nolint: tagliatelle
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScheduleTrigger `json:"items"`
}

//nolint: gochecknoinits
func init() {
	SchemeBuilder.Register(&ScheduleTrigger{}, &ScheduleTriggerList{})
}
