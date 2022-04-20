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

package scheduler

import "errors"

var (
	// ErrNotFound is returned when a resource is not found.
	ErrNotFound = errors.New("not-found")

	// ErrNoPodSetFound returned when podset is not found.
	ErrNoPodSetFound = errors.New("no-podset-found")

	// ErrNoPlannersFound returned when no planners are present for pod.
	ErrNoPlannersFound = errors.New("no-planners")

	// ErrNilAssignmentState returned when a pod has a nil assignment.
	ErrNilAssignmentState = errors.New("nil-assignment-state")

	// ErrInvalidAssignmentState returned when the assignment is not of a
	// valid type.
	ErrInvalidAssignmentState = errors.New("invalid-assignment-state")

	// ErrBuildingPlan returned when there was an error trying to build schedule plan.
	ErrBuildingPlan = errors.New("plan-build-failure")

	// ErrPodNotAssignable returned when a pod cannot be scheduled to a node.
	ErrPodNotAssignable = errors.New("pod-cannot-be-assigned")
)
