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
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	schedschemebuilder "k8s.io/kube-scheduler/config/v1beta2"
	schedschemeinternalbuilder "k8s.io/kubernetes/pkg/scheduler/apis/config"
	schedscheme "k8s.io/kubernetes/pkg/scheduler/apis/config/v1beta2"
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(schedschemebuilder.SchemeGroupVersion,
		&ConstraintPolicySchedulingArgs{})
	scheme.AddKnownTypes(schedschemeinternalbuilder.SchemeGroupVersion,
		&ConstraintPolicySchedulingArgs{})

	return nil
}

// nolint:gochecknoinits
func init() {
	localSchemeBuilder := &schedschemebuilder.SchemeBuilder
	localSchemeBuilder.Register(addKnownTypes)

	scheme := schedscheme.GetPluginArgConversionScheme()
	utilruntime.Must(localSchemeBuilder.AddToScheme(scheme))
}
