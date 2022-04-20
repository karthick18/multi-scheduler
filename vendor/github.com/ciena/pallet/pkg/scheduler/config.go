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

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodSetPlannerArgs defines the parameters for the scheduling plugin
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PodSetPlannerArgs struct {
	// nolint:tagliatelle
	metav1.TypeMeta `json:",inline"`
	Debug           bool   `json:"debug,omitempty"`
	CallTimeout     string `json:"callTimeout,omitempty"`
	UpdatePeriod    string `json:"updatePeriod,omitempty"`
}

// PodSetPlannerOptions is a set of options used while initializing a podset scheduler.
type PodSetPlannerOptions struct {
	Debug        bool
	CallTimeout  time.Duration
	UpdatePeriod time.Duration
}

// DefaultPodSetPlannerConfig returns the default options for podset scheduler.
func DefaultPodSetPlannerConfig() *PodSetPlannerOptions {
	// nolint:gomnd
	return &PodSetPlannerOptions{
		Debug:        true,
		CallTimeout:  time.Second * 15,
		UpdatePeriod: time.Second * 5,
	}
}

type durationAndRef struct {
	duration string
	ref      *time.Duration
}

func parsePluginConfig(pluginConfig *PodSetPlannerArgs, defaultConfig *PodSetPlannerOptions) *PodSetPlannerOptions {
	config := *defaultConfig

	config.Debug = pluginConfig.Debug

	durationRefs := []*durationAndRef{
		{duration: pluginConfig.CallTimeout, ref: &config.CallTimeout},
		{duration: pluginConfig.UpdatePeriod, ref: &config.UpdatePeriod},
	}

	for _, durationRef := range durationRefs {
		if durationRef.duration == "" {
			continue
		}

		if d, err := time.ParseDuration(durationRef.duration); err == nil && d > time.Duration(0) {
			*durationRef.ref = d
		}
	}

	return &config
}
