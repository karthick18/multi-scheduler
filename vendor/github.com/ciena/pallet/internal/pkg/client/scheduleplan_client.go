/*
Copyright 2022 Ciena Corporation..

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

package client

import (
	"context"
	"fmt"

	plannerv1alpha1 "github.com/ciena/pallet/pkg/apis/scheduleplanner/v1alpha1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SchedulePlannerClient stores schedule planner client info.
type SchedulePlannerClient struct {
	Client client.Client
	Log    logr.Logger
}

// NewPlannerClient creates a new schedule planner client instance.
func NewPlannerClient(c client.Client, log logr.Logger) *SchedulePlannerClient {
	return &SchedulePlannerClient{Client: c, Log: log}
}

// List lists the schedule planner resources by namespace and labels.
func (c *SchedulePlannerClient) List(ctx context.Context,
	namespace string,
	labels map[string]string) (*plannerv1alpha1.SchedulePlanList, error) {
	var planList plannerv1alpha1.SchedulePlanList

	err := c.Client.List(ctx, &planList,
		client.InNamespace(namespace),
		client.MatchingLabels(labels))
	if err != nil {
		c.Log.Error(err, "error-listing-planner", "namespace", namespace, "labels", labels)

		return nil, fmt.Errorf("error listing planner: %w", err)
	}

	return &planList, nil
}

// Get gets the schedule planner resource by namespace and podset.
func (c *SchedulePlannerClient) Get(ctx context.Context,
	namespace, podset string) (*plannerv1alpha1.SchedulePlan, error) {
	var plan plannerv1alpha1.SchedulePlan

	name := getName(namespace, podset)

	err := c.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &plan)
	if err != nil {
		return nil, fmt.Errorf("error getting planner: %w", err)
	}

	return &plan, nil
}

// Update updates or creates planner spec assignments by namespace and podset.
func (c *SchedulePlannerClient) Update(ctx context.Context,
	namespace, podset string,
	assignments map[string]string,
) error {
	planRef, err := c.Get(ctx, namespace, podset)
	if err != nil {
		var newPlan []plannerv1alpha1.PlanSpec

		for pod, node := range assignments {
			newPlan = append(newPlan,
				plannerv1alpha1.PlanSpec{Pod: pod, Node: node})
		}

		name := getName(namespace, podset)
		plan := plannerv1alpha1.SchedulePlan{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
				Labels: map[string]string{
					"planner.ciena.io/pod-set": podset,
				},
			},
			Spec: plannerv1alpha1.SchedulePlanSpec{
				Plan: newPlan,
			},
		}

		err = c.Client.Create(ctx, &plan)
		if err != nil {
			c.Log.Error(err, "error-creating-schedule-plan", "podset", podset)

			return fmt.Errorf("error creating plan spec: %w", err)
		}

		c.Log.V(1).Info("plan-create-success", "name", name, "podset", podset)

		return nil
	}

	// update the plan spec if pod and node assignment has changed
	var newPlan []plannerv1alpha1.PlanSpec

	var currentPlan []plannerv1alpha1.PlanSpec

	presenceMap := make(map[plannerv1alpha1.PlanSpec]struct{}, len(planRef.Spec.Plan))

	for i := range planRef.Spec.Plan {
		presenceMap[planRef.Spec.Plan[i]] = struct{}{}
	}

	for pod, node := range assignments {
		planSpec := plannerv1alpha1.PlanSpec{Pod: pod, Node: node}

		if _, ok := presenceMap[planSpec]; !ok {
			newPlan = append(newPlan, planSpec)
		} else {
			currentPlan = append(currentPlan, planSpec)
		}
	}

	if len(newPlan) == 0 {
		c.Log.V(1).Info("no-changes-in-planner-assignments", "podset", podset, "namespace", namespace)

		return nil
	}

	currentPlan = append(currentPlan, newPlan...)
	planRef.Spec.Plan = currentPlan

	err = c.Client.Update(ctx, planRef)
	if err != nil {
		c.Log.Error(err, "plan-crud-update-error", "name", planRef.Name,
			"podset", podset, "namespace", namespace)

		return fmt.Errorf("error updating plan spec: %w", err)
	}

	c.Log.V(1).Info("plan-update-success", "name", planRef.Name, "podset", podset)

	return nil
}

// UpdateAssignment updates or creates planner spec assignment for the pod with the nodename.
func (c *SchedulePlannerClient) UpdateAssignment(ctx context.Context,
	namespace, podset string,
	podName, nodeName string,
) error {
	planRef, err := c.Get(ctx, namespace, podset)
	if err != nil {
		newPlan := []plannerv1alpha1.PlanSpec{
			{
				Pod:  podName,
				Node: nodeName,
			},
		}

		name := getName(namespace, podset)
		plan := plannerv1alpha1.SchedulePlan{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
				Labels: map[string]string{
					"planner.ciena.io/pod-set": podset,
				},
			},
			Spec: plannerv1alpha1.SchedulePlanSpec{
				Plan: newPlan,
			},
		}

		err = c.Client.Create(ctx, &plan)
		if err != nil {
			c.Log.Error(err, "error-creating-schedule-plan", "podset", podset,
				"pod", podName, "node", nodeName)

			return fmt.Errorf("error creating plan spec for pod %s, node %s: %w",
				podName, nodeName, err)
		}

		c.Log.V(1).Info("plan-create-success", "name", name, "podset", podset,
			"pod", podName, "node", nodeName)

		return nil
	}

	var newPlan []plannerv1alpha1.PlanSpec

	var currentPlan []plannerv1alpha1.PlanSpec

	for i := range planRef.Spec.Plan {
		plan := planRef.Spec.Plan[i]

		// update the plan spec if there is an assignment change.
		if plan.Pod == podName && plan.Node != nodeName {
			newPlan = append(newPlan, plannerv1alpha1.PlanSpec{
				Pod:  podName,
				Node: nodeName,
			})
		} else {
			currentPlan = append(currentPlan, plan)
		}
	}

	if len(newPlan) == 0 {
		c.Log.V(1).Info("no-changes-in-planner-assignment", "pod", podName, "node", nodeName)

		return nil
	}

	currentPlan = append(currentPlan, newPlan...)
	planRef.Spec.Plan = currentPlan

	err = c.Client.Update(ctx, planRef)
	if err != nil {
		c.Log.Error(err, "plan-crud-update-error", "name", planRef.Name,
			"podset", podset, "namespace", namespace, "pod", podName, "node", nodeName)

		return fmt.Errorf("error updating plan spec for pod %s, node %s: %w", podName, nodeName, err)
	}

	c.Log.V(1).Info("plan-update-success", "name", planRef.Name, "pod", podName, "node", nodeName)

	return nil
}

// Delete deletes schedule planner spec by podname, namespace and podset.
func (c *SchedulePlannerClient) Delete(ctx context.Context, podName, namespace, podset string) (bool, error) {
	planRef, err := c.Get(ctx, namespace, podset)
	if err != nil {
		c.Log.Error(err, "no-planner-instance-found", "pod", podName, "podset", podset, "namespace", namespace)

		return false, err
	}

	var newPlan []plannerv1alpha1.PlanSpec

	found := false

	for i := range planRef.Spec.Plan {
		if planRef.Spec.Plan[i].Pod == podName {
			found = true
		} else {
			newPlan = append(newPlan, planRef.Spec.Plan[i])
		}
	}

	// nothing to update
	if !found {
		return false, nil
	}

	if len(newPlan) == 0 {
		// delete the plan
		plan := &plannerv1alpha1.SchedulePlan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      planRef.Name,
				Namespace: planRef.Namespace,
			},
		}

		err = c.Client.Delete(ctx, plan)
		if err != nil {
			c.Log.Error(err, "error-deleting-plan", "plan", planRef.Name, "podset", podset)

			return true, fmt.Errorf("error deleting plan: %w", err)
		}

		return false, nil
	}

	planRef.Spec.Plan = newPlan

	err = c.Client.Update(ctx, planRef)
	if err != nil {
		c.Log.Error(err, "error-updating-plan-spec", "pod", podName, "podset", podset)

		return true, fmt.Errorf("error updating plan spec: %w", err)
	}

	return false, nil
}

// CheckIfPodPresent checks if podname is present in the plan spec.
func (c *SchedulePlannerClient) CheckIfPodPresent(ctx context.Context,
	namespace,
	podset,
	podName string,
) (bool, *plannerv1alpha1.PlanSpec, error) {
	plan, err := c.Get(ctx, namespace, podset)
	if err != nil {
		return false, nil, err
	}

	for i := range plan.Spec.Plan {
		if plan.Spec.Plan[i].Pod == podName {
			return true, &plan.Spec.Plan[i], nil
		}
	}

	return false, nil, nil
}
