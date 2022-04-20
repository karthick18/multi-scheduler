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
package main

import (
	"os"

	ps "github.com/ciena/pallet/pkg/scheduler"
	cps "github.com/ciena/turnbuckle/pkg/scheduler"
	"k8s.io/component-base/logs"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"
)

func main() {
	// Register custom plugins to the scheduler framework.
	command := app.NewSchedulerCommand(
		app.WithPlugin(ps.Name, ps.New),
		app.WithPlugin(cps.Name, cps.New),
	)

	logs.InitLogs()

	if err := command.Execute(); err != nil {
		logs.FlushLogs()
		os.Exit(1)
	}

	logs.FlushLogs()
}
