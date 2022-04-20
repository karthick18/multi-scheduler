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
	// ErrNotFound returned when specified resource is not found.
	ErrNotFound = errors.New("not-found")

	// ErrNoOffers returned when no offers are returned for pod.
	ErrNoOffers = errors.New("no-offers")

	// ErrNoNodesFound returned when no elible nodes are found for pod.
	ErrNoNodesFound = errors.New("no eligible nodes found")

	// ErrNoCost returned when no cost can be calculated.
	ErrNoCost = errors.New("no-cost")

	// ErrNodeNameNotFound returned when a node name cannot be found for a node.
	ErrNodeNameNotFound = errors.New("no-nodename-found")

	// ErrPodNotAssigned returned when a pod has net yet been assigned to
	// a node.
	ErrPodNotAssigned = errors.New("pod-not-assigned")

	// ErrNilAssignmentState returned when a pod has a nil assignment.
	ErrNilAssignmentState = errors.New("nil-assignment-state")

	// ErrInvalidAssignmentState returned when the assignment is not of a
	// valid type.
	ErrInvalidAssignmentState = errors.New("invalid-assignment-state")
)
