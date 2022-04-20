/*
Copyright 2021 Ciena Corporation.

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

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConstraintPolicySchedulingArgs defines the parameters for ConstraintPolicyScheduling plugin.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ConstraintPolicySchedulingArgs struct {
	// nolint:tagliatelle
	metav1.TypeMeta      `json:",inline"`
	Debug                bool   `json:"debug,omitempty"`
	MinDelayOnFailure    string `json:"minDelayOnFailure,omitempty"`
	MaxDelayOnFailure    string `json:"maxDelayOnFailure,omitempty"`
	NumRetriesOnFailure  int    `json:"numRetriesOnFailure,omitempty"`
	FallbackOnNoOffers   bool   `json:"fallbackOnNoOffers,omitempty"`
	RetryOnNoOffers      bool   `json:"retryOnNoOffers,omitempty"`
	RequeuePeriod        string `json:"requeuePeriod,omitempty"`
	PlannerNodeQueueSize uint   `json:"plannerNodeQueueSize,omitempty"`
	CallTimeout          string `json:"callTimeout,omitempty"`
	UpdateWorkerPeriod   string `json:"updateWorkerPeriod,omitempty"`
}

// DefaultConstraintPolicySchedulerConfig returns the default options for scheduler.
func DefaultConstraintPolicySchedulerConfig() *ConstraintPolicySchedulerOptions {
	// nolint:gomnd
	return &ConstraintPolicySchedulerOptions{
		Debug:                true,
		NumRetriesOnFailure:  3,
		MinDelayOnFailure:    30 * time.Second,
		MaxDelayOnFailure:    time.Minute,
		FallbackOnNoOffers:   false,
		RetryOnNoOffers:      false,
		RequeuePeriod:        5 * time.Second,
		PlannerNodeQueueSize: 300,
		CallTimeout:          15 * time.Second,
		UpdateWorkerPeriod:   5 * time.Second,
	}
}

type durationAndRef struct {
	duration string
	ref      *time.Duration
}

func parsePluginConfig(pluginConfig *ConstraintPolicySchedulingArgs,
	defaultConfig *ConstraintPolicySchedulerOptions) *ConstraintPolicySchedulerOptions {
	config := *defaultConfig

	config.Debug = pluginConfig.Debug
	config.FallbackOnNoOffers = pluginConfig.FallbackOnNoOffers
	config.RetryOnNoOffers = pluginConfig.RetryOnNoOffers

	if pluginConfig.NumRetriesOnFailure > 0 {
		config.NumRetriesOnFailure = pluginConfig.NumRetriesOnFailure
	}

	durationRefs := []*durationAndRef{
		{duration: pluginConfig.MinDelayOnFailure, ref: &config.MinDelayOnFailure},
		{duration: pluginConfig.MaxDelayOnFailure, ref: &config.MaxDelayOnFailure},
		{duration: pluginConfig.RequeuePeriod, ref: &config.RequeuePeriod},
		{duration: pluginConfig.CallTimeout, ref: &config.CallTimeout},
		{duration: pluginConfig.UpdateWorkerPeriod, ref: &config.UpdateWorkerPeriod},
	}

	for _, durationRef := range durationRefs {
		if durationRef.duration == "" {
			continue
		}

		if d, err := time.ParseDuration(durationRef.duration); err == nil && d > time.Duration(0) {
			*durationRef.ref = d
		}
	}

	if config.MinDelayOnFailure >= config.MaxDelayOnFailure {
		//nolint:gomnd
		config.MinDelayOnFailure = config.MaxDelayOnFailure / 2
	}

	if pluginConfig.PlannerNodeQueueSize > 0 {
		config.PlannerNodeQueueSize = pluginConfig.PlannerNodeQueueSize
	}

	return &config
}
