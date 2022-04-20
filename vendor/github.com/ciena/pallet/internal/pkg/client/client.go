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
	"fmt"

	plannerv1alpha1 "github.com/ciena/pallet/pkg/apis/scheduleplanner/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	ctlrclient "sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:gochecknoglobals
var (
	plannerScheme = runtime.NewScheme()

	triggerScheme = runtime.NewScheme()
)

//nolint:gochecknoinits
func init() {
	schemes := []*runtime.Scheme{plannerScheme, triggerScheme}

	for _, scheme := range schemes {
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(plannerv1alpha1.AddToScheme(scheme))
	}
}

// NewSchedulePlannerClient creates a new client for schedule planner resource.
func NewSchedulePlannerClient(config *rest.Config, log logr.Logger) (*SchedulePlannerClient, error) {
	genericClient, err := ctlrclient.New(config,
		ctlrclient.Options{
			Scheme: plannerScheme,
		})
	if err != nil {
		return nil, fmt.Errorf("error creating controller client: %w", err)
	}

	return NewPlannerClient(genericClient, log), nil
}

// NewScheduleTriggerClient creates a new client for schedule trigger resource.
func NewScheduleTriggerClient(config *rest.Config, log logr.Logger) (*ScheduleTriggerClient, error) {
	genericClient, err := ctlrclient.New(config,
		ctlrclient.Options{
			Scheme: triggerScheme,
		})
	if err != nil {
		return nil, fmt.Errorf("error creating controller client: %w", err)
	}

	return NewTriggerClient(genericClient, log), nil
}
