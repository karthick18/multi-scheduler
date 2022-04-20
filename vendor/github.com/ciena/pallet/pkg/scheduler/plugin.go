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
	"context"
	"errors"
	"fmt"

	"github.com/ciena/pallet/internal/pkg/client"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	// Name of the scheduling plugin.
	Name                      = "PodSetPlanner"
	plannerAssignmentStateKey = Name + "AssignmentState"
)

// PodSetPlanner instance state for the the policy scheduling
// plugin.
type PodSetPlanner struct {
	handle         framework.Handle
	log            logr.Logger
	options        *PodSetPlannerOptions
	plannerService *PlannerService
	plannerClient  *client.SchedulePlannerClient
	triggerClient  *client.ScheduleTriggerClient
}

type plannerAssignmentState struct {
	node string
}

var (
	_ framework.PreFilterPlugin  = &PodSetPlanner{}
	_ framework.PreScorePlugin   = &PodSetPlanner{}
	_ framework.ScorePlugin      = &PodSetPlanner{}
	_ framework.PostFilterPlugin = &PodSetPlanner{}
	_ framework.ReservePlugin    = &PodSetPlanner{}
)

// New create a new framework plugin intance.
// nolint:ireturn
func New(
	obj runtime.Object, handle framework.Handle) (framework.Plugin, error,
) {
	var log logr.Logger

	var config *PodSetPlannerOptions

	defaultConfig := DefaultPodSetPlannerConfig()

	if obj != nil {
		//nolint: forcetypeassert
		pluginConfig := obj.(*PodSetPlannerArgs)
		config = parsePluginConfig(pluginConfig, defaultConfig)
	} else {
		config = defaultConfig
	}

	if config.Debug {
		zapLog, err := zap.NewDevelopment()
		if err != nil {
			return nil, fmt.Errorf("error creating dev logger: %w", err)
		}

		log = zapr.NewLogger(zapLog)
	} else {
		zapLog, err := zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("error creating prod logger: %w", err)
		}

		log = zapr.NewLogger(zapLog)
	}

	pluginLogger := log.WithName("scheduling-plugin")

	restConfig := *handle.KubeConfig()
	restConfig.ContentType = "application/json"

	plannerClient, err := client.NewSchedulePlannerClient(&restConfig,
		pluginLogger.WithName("planner-client"))
	if err != nil {
		pluginLogger.Error(err, "error-initializing-planner-client")

		//nolint:wrapcheck
		return nil, err
	}

	triggerClient, err := client.NewScheduleTriggerClient(&restConfig,
		pluginLogger.WithName("trigger-client"))
	if err != nil {
		pluginLogger.Error(err, "error-initializing-trigger-client")

		//nolint:wrapcheck
		return nil, err
	}

	plannerService := NewPlannerService(plannerClient, handle,
		pluginLogger.WithName("planner-service"),
		config.CallTimeout)

	schedulePlanner := &PodSetPlanner{
		handle:         handle,
		log:            pluginLogger,
		options:        config,
		plannerService: plannerService,
		plannerClient:  plannerClient,
		triggerClient:  triggerClient,
	}

	pluginLogger.V(1).Info("podset-scheduling-plugin-initialized")

	return schedulePlanner, nil
}

func getAssignmentState(cycleState *framework.CycleState) (*plannerAssignmentState, error) {
	state, err := cycleState.Read(plannerAssignmentStateKey)
	if err != nil {
		return nil, fmt.Errorf("unable to read cycle state: %w", err)
	}

	if state == nil {
		return nil, ErrNilAssignmentState
	}

	assignmentState, ok := state.(*plannerAssignmentState)
	if !ok {
		return nil, fmt.Errorf("%+v convert to node assignment state error: %w",
			state, ErrInvalidAssignmentState)
	}

	return assignmentState, nil
}

// Clone isn't needed for our state data.
// nolint:ireturn
func (s *plannerAssignmentState) Clone() framework.StateData {
	return s
}

func (p *PodSetPlanner) createAssignmentState(
	ctx context.Context,
	pod *v1.Pod,
	eligibleNodes []*v1.Node) (*plannerAssignmentState, *framework.Status,
) {
	if len(eligibleNodes) == 0 {
		p.log.V(1).Info("create-assignment-state-no-nodes-eligible")

		return nil, framework.NewStatus(framework.Unschedulable)
	}

	nodeNames := make([]string, len(eligibleNodes))

	for i, nodeInfo := range eligibleNodes {
		nodeNames[i] = nodeInfo.Name
	}

	selectedNode, err := p.findFit(ctx, pod, nodeNames)
	if err != nil {
		return nil, framework.AsStatus(err)
	}

	return &plannerAssignmentState{node: selectedNode}, framework.NewStatus(framework.Success)
}

// Name returns the name of the scheduler.
func (p *PodSetPlanner) Name() string {
	return Name
}

// PreFilter pre-filters the pods to be placed.
func (p *PodSetPlanner) PreFilter(
	parentCtx context.Context,
	_ *framework.CycleState,
	pod *v1.Pod) *framework.Status {

	p.log.V(1).Info("prefilter", "pod", pod.Name)

	err := p.canSchedulePod(parentCtx, pod)
	if err != nil {
		if errors.Is(err, ErrNoPodSetFound) {
			return framework.NewStatus(framework.Success)
		}

		return framework.AsStatus(err)
	}

	return framework.NewStatus(framework.Success)
}

// PreFilterExtensions returns prefilter extensions, pod add and remove.
// nolint:ireturn
func (p *PodSetPlanner) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// PreScore uses the filtered node list and selects the node for the pod using the planner.
func (p *PodSetPlanner) PreScore(
	parentCtx context.Context,
	state *framework.CycleState,
	pod *v1.Pod,
	nodes []*v1.Node) *framework.Status {
	p.log.V(1).Info("pre-score", "pod", pod.Name, "nodes", len(nodes))

	//nolint: gomnd
	ctx, cancel := context.WithTimeout(parentCtx, p.options.CallTimeout*2)
	defer cancel()

	assignmentState, status := p.createAssignmentState(ctx, pod, nodes)
	if !status.IsSuccess() {
		// check if the pod does not belong to a podset, or no planners found.
		// in that case, we allow the default scheduler to schedule the pod
		if status.Equal(framework.AsStatus(ErrNoPodSetFound)) || status.Equal(framework.AsStatus(ErrNoPlannersFound)) {
			return framework.NewStatus(framework.Success)
		}

		return framework.NewStatus(framework.Unschedulable)
	}

	p.log.V(1).Info("prescore-state-assignment", "pod", pod.Name, "node", assignmentState.node)
	state.Write(plannerAssignmentStateKey, assignmentState)

	return status
}

// Score scores the eligible nodes.
func (p *PodSetPlanner) Score(
	ctx context.Context,
	state *framework.CycleState,
	pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	p.log.V(1).Info("score", "pod", pod.Name, "node", nodeName)

	assignmentState, err := getAssignmentState(state)
	if err != nil {
		return framework.MinNodeScore, framework.NewStatus(framework.Success)
	}

	if assignmentState.node != nodeName {
		return 1, framework.NewStatus(framework.Success)
	}

	p.log.V(1).Info("set-score", "score", framework.MaxNodeScore, "pod", pod.Name, "node", nodeName)

	return framework.MaxNodeScore, framework.NewStatus(framework.Success)
}

// ScoreExtensions calcuates scores for the extensions.
// nolint:ireturn
func (p *PodSetPlanner) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// PostFilter is called when no node can be assigned to the pod.
func (p *PodSetPlanner) PostFilter(ctx context.Context,
	state *framework.CycleState, pod *v1.Pod,
	filteredNodeStatusMap framework.NodeToStatusMap) (*framework.PostFilterResult, *framework.Status,
) {
	p.log.V(1).Info("post-filter", "pod", pod.Name)

	assignmentState, err := getAssignmentState(state)
	if err != nil {
		return nil, framework.AsStatus(err)
	}

	p.log.V(1).Info("post-filter", "nominated-node", assignmentState.node, "pod", pod.Name)

	return &framework.PostFilterResult{NominatedNodeName: assignmentState.node}, framework.NewStatus(framework.Success)
}

// Reserve is called when the scheduler cache is updated with the nodename for the pod.
// We do planner assignment state validations for the pod.
func (p *PodSetPlanner) Reserve(parentCtx context.Context,
	state *framework.CycleState,
	pod *v1.Pod,
	nodeName string,
) *framework.Status {
	p.log.V(1).Info("reserve", "pod", pod.Name, "node", nodeName)

	assignmentState, err := getAssignmentState(state)
	if err != nil {
		return framework.NewStatus(framework.Success)
	}

	podset := p.getPodSet(pod)
	if podset == "" {
		// if assignment state exists, podset has to be present ideally.
		// we fail the reservation and retry planner assignment.
		return framework.AsStatus(ErrNoPodSetFound)
	}

	// see if there is a mismatch between planner assignment and actual.
	if assignmentState.node != nodeName {
		p.log.V(1).Info("reserve-update-assignment", "pod", pod.Name, "node", nodeName)

		err := p.plannerService.UpdateAssignment(parentCtx, pod, podset, nodeName)
		if err != nil {
			// we fail reservation on planspec update failure to retry assignment.
			return framework.AsStatus(err)
		}

		assignmentState.node = nodeName
		state.Write(plannerAssignmentStateKey, assignmentState)
	}

	return framework.NewStatus(framework.Success)
}

// Unreserve is called when reserved pod was rejected or on reserve error.
// We undo the assignment and update planner spec by removing the pod assignment.
func (p *PodSetPlanner) Unreserve(parentCtx context.Context, state *framework.CycleState,
	pod *v1.Pod,
	nodeName string,
) {
	p.log.V(1).Info("unreserve", "pod", pod.Name, "node", nodeName)

	_, err := getAssignmentState(state)
	if err != nil {
		return
	}

	podset := p.getPodSet(pod)
	if podset == "" {
		return
	}

	err = p.plannerService.Delete(parentCtx, pod, podset, nodeName)
	if err != nil {
		p.log.V(1).Info("unreserve-delete-assignment-failed", "pod", pod.Name, "node", nodeName)

		return
	}

	state.Delete(plannerAssignmentStateKey)
	p.log.V(1).Info("unreserve-delete-assignment-success", "pod", pod.Name, "node", nodeName)
}

func (p *PodSetPlanner) canSchedulePod(parentCtx context.Context, pod *v1.Pod) error {
	podset := p.getPodSet(pod)
	if podset == "" {
		return ErrNoPodSetFound
	}

	ctx, cancel := context.WithTimeout(parentCtx, p.options.CallTimeout)
	trigger, err := p.triggerClient.Get(ctx, pod.Namespace, podset)

	cancel()

	if err != nil {
		p.log.V(1).Info("no-trigger-found", "pod", pod.Name, "podset", podset, "namespace", pod.Namespace)

		//nolint: wrapcheck
		return err
	}

	// pod not assignable if trigger is not active
	if trigger.Spec.State != "Schedule" {
		p.log.V(1).Info("trigger-not-active", "trigger", trigger.Name, "state", trigger.Spec.State, "podset", podset)

		return ErrPodNotAssignable
	}

	return nil
}

func (p *PodSetPlanner) findFit(parentCtx context.Context, pod *v1.Pod, eligibleNodes []string) (string, error) {
	podset := p.getPodSet(pod)
	if podset == "" {
		return "", ErrNoPodSetFound
	}

	ctx, cancel := context.WithTimeout(parentCtx, p.options.CallTimeout)
	trigger, err := p.triggerClient.Get(ctx, pod.Namespace, podset)

	cancel()

	if err != nil {
		p.log.V(1).Info("no-trigger-found", "pod", pod.Name, "podset", podset, "namespace", pod.Namespace)

		//nolint: wrapcheck
		return "", err
	}

	// pod not assignable if trigger is not active
	if trigger.Spec.State != "Schedule" {
		p.log.V(1).Info("trigger-not-active", "trigger", trigger.Name, "state", trigger.Spec.State, "podset", podset)

		return "", ErrPodNotAssignable
	}

	ctx, cancel = context.WithTimeout(parentCtx, p.options.CallTimeout)
	planStatus, planSpec, _ := p.plannerClient.CheckIfPodPresent(ctx,
		pod.Namespace, podset, pod.Name)

	cancel()

	// pod is in the planspec.
	// check if the node is in the eligible filtered list
	// if not, we hit the planner again to reallocate pod
	// with the new eligible set
	if planStatus {
		for _, node := range eligibleNodes {
			if node == planSpec.Node {
				return planSpec.Node, nil
			}
		}
	}

	p.log.V(1).Info("planner-lookup", "pod", pod.Name, "namespace", pod.Namespace,
		"podset", podset)

	// get to the planner to place the pod
	planners, err := p.plannerService.Lookup(parentCtx, pod.Namespace, podset,
		pod.Name, eligibleNodes)
	if err != nil {
		p.log.V(1).Info("could-not-find-planner", "pod", pod.Name, "podset", podset)

		return "", err
	}

	p.log.V(1).Info("found-planner", "pod", pod.Name, "podset", podset, "planners", len(planners))

	assignments, err := planners.Invoke(parentCtx, trigger)
	if err != nil {
		return "", err
	}

	// planner assignment success. validate if this pod is in the assignment map
	selectedNode, ok := assignments[pod.Name]
	if !ok {
		return "", ErrNotFound
	}

	err = p.plannerService.Update(parentCtx, pod, podset, assignments)
	if err != nil {
		return "", err
	}

	p.log.V(1).Info("scheduler-assignment", "pod", pod.Name, "node", selectedNode)

	return selectedNode, nil
}

func (p *PodSetPlanner) getPodSet(pod *v1.Pod) string {
	podset := ""

	for k, v := range pod.Labels {
		if k == "planner.ciena.io/pod-set" {
			podset = v

			break
		}
	}

	return podset
}
