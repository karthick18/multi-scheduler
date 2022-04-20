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

package client

import (
	"context"
	"errors"
	"fmt"

	plannerv1alpha1 "github.com/ciena/pallet/pkg/apis/scheduleplanner/v1alpha1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrNoTriggersFound is returned when no triggers are found.
var ErrNoTriggersFound = errors.New("no triggers found")

// ScheduleTriggerClient saves schedule trigger client specific info.
type ScheduleTriggerClient struct {
	Client client.Client
	Log    logr.Logger
}

// NewTriggerClient creates a schedule trigger client instance.
func NewTriggerClient(c client.Client, log logr.Logger) *ScheduleTriggerClient {
	return &ScheduleTriggerClient{Client: c, Log: log}
}

// List lists the schedule trigger instances by namespace and labels.
func (c *ScheduleTriggerClient) List(ctx context.Context,
	namespace string,
	labels map[string]string) (*plannerv1alpha1.ScheduleTriggerList, error) {
	var triggerList plannerv1alpha1.ScheduleTriggerList

	err := c.Client.List(ctx, &triggerList,
		client.InNamespace(namespace),
		client.MatchingLabels(labels))
	if err != nil {
		c.Log.Error(err, "error-listing-trigger", "namespace", namespace, "labels", labels)

		return nil, fmt.Errorf("error list triggers: %w", err)
	}

	return &triggerList, nil
}

// Get is used to get the schedule trigger instance by namespace and podset.
func (c *ScheduleTriggerClient) Get(ctx context.Context,
	namespace,
	podset string,
) (*plannerv1alpha1.ScheduleTrigger, error) {
	triggers, err := c.List(ctx, namespace, map[string]string{"planner.ciena.io/pod-set": podset})
	if err != nil {
		c.Log.Error(err, "error-getting-trigger", "namespace", namespace, "podset", podset)

		return nil, err
	}

	if len(triggers.Items) == 0 {
		return nil, fmt.Errorf("no-trigger-found-for-podset-%s: %w",
			podset, ErrNoTriggersFound)
	}

	return &triggers.Items[0], nil
}

// Update updates the schedule trigger client resource by namespace and podset.
func (c *ScheduleTriggerClient) Update(ctx context.Context,
	namespace,
	podset,
	state string,
) error {
	trigger, err := c.Get(ctx, namespace, podset)
	if err != nil {
		return err
	}

	// update state
	if trigger.Spec.State == state {
		return nil
	}

	trigger.Spec.State = state

	err = c.Client.Update(ctx, trigger)
	if err != nil {
		c.Log.Error(err, "trigger-crud-update-error", "name", trigger.Name, "podset", podset, "state", state)

		return fmt.Errorf("error updating trigger: %w", err)
	}

	c.Log.V(1).Info("trigger-update-success", "name", trigger.Name, "podset", podset, "state", state)

	return nil
}
