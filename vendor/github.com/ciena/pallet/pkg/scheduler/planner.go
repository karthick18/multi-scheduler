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

package scheduler

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/ciena/pallet/internal/pkg/client"
	planner "github.com/ciena/pallet/pkg/apis/planner"
	plannerv1alpha1 "github.com/ciena/pallet/pkg/apis/scheduleplanner/v1alpha1"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// Planner is used to store info to lookup podset planner service.
type Planner struct {
	Service       v1.Service
	Namespace     string
	Podset        string
	ScheduledPod  string
	EligibleNodes []string
	Log           logr.Logger
	CallTimeout   time.Duration
	DialOptions   []grpc.DialOption
}

// PlannerList is a list of references to Planner.
type PlannerList []*Planner

// PlannerService is used to lookup a podset planner service.
type PlannerService struct {
	clnt        *client.SchedulePlannerClient
	handle      framework.Handle
	log         logr.Logger
	callTimeout time.Duration
}

type planReward struct {
	reward      int
	assignments map[string]string
}

// NewPlannerService is used to create a new planner service for the podset.
func NewPlannerService(clnt *client.SchedulePlannerClient, handle framework.Handle,
	log logr.Logger, callTimeout time.Duration,
) *PlannerService {
	return &PlannerService{clnt: clnt, handle: handle, log: log, callTimeout: callTimeout}
}

// Update is used to create or update the plan spec for the podset planner.
func (s *PlannerService) Update(parentCtx context.Context, pod *v1.Pod,
	podset string,
	assignments map[string]string,
) error {
	ctx, cancel := context.WithTimeout(parentCtx, s.callTimeout)
	defer cancel()

	err := s.clnt.Update(ctx,
		pod.Namespace,
		podset,
		assignments)
	if err != nil {
		s.log.Error(err, "update-plan-failure", "pod", pod.Name)

		//nolint: wrapcheck
		return err
	}

	return nil
}

// UpdateAssignment is used to update plan spec with the pod and node assignment.
func (s *PlannerService) UpdateAssignment(parentCtx context.Context, pod *v1.Pod,
	podset string,
	nodeName string,
) error {
	ctx, cancel := context.WithTimeout(parentCtx, s.callTimeout)
	defer cancel()

	err := s.clnt.UpdateAssignment(ctx,
		pod.Namespace,
		podset,
		pod.Name,
		nodeName)
	if err != nil {
		s.log.Error(err, "update-plan-assignment-failure", "pod", pod.Name, "node", nodeName)

		//nolint: wrapcheck
		return err
	}

	return nil
}

// Delete is used to delete the assignment for the pod from the planspec.
func (s *PlannerService) Delete(parentCtx context.Context,
	pod *v1.Pod,
	podset string,
	_ string,
) error {
	ctx, cancel := context.WithTimeout(parentCtx, s.callTimeout)
	defer cancel()

	if _, err := s.clnt.Delete(ctx, pod.Name, pod.Namespace, podset); err != nil {
		return fmt.Errorf("error deleting pod %s from plan spec: %w", pod.Name, err)
	}

	return nil
}

func (s *PlannerService) lookupWithLabelSelector(parentCtx context.Context,
	labelSelector string,
	namespace, podset, scheduledPod string,
	eligibleNodes []string) (PlannerList, error,
) {
	ctx, cancel := context.WithTimeout(parentCtx, s.callTimeout)
	defer cancel()

	svcs, err := s.handle.ClientSet().CoreV1().Services("").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("error getting service list: %w", err)
	}

	if len(svcs.Items) == 0 {
		return nil, ErrNoPlannersFound
	}

	planners := make(PlannerList, len(svcs.Items))

	for index := range svcs.Items {
		svc := &svcs.Items[index]

		planners[index] = &Planner{
			Service:       *svc,
			Namespace:     namespace,
			Podset:        podset,
			ScheduledPod:  scheduledPod,
			EligibleNodes: eligibleNodes,
			Log:           s.log.WithName("podset-planner"),
			CallTimeout:   s.callTimeout,
			DialOptions: []grpc.DialOption{
				grpc.WithInsecure(),
			},
		}
	}

	return planners, nil
}

// Lookup is used to lookup a podset planner service.
func (s *PlannerService) Lookup(parentCtx context.Context,
	namespace, podset, scheduledPod string, eligibleNodes []string) (PlannerList, error,
) {
	podSetLabel := fmt.Sprintf("planner.ciena.io/%s=enabled", podset)

	planners, err := s.lookupWithLabelSelector(parentCtx, podSetLabel,
		namespace, podset, scheduledPod, eligibleNodes)
	if err != nil {
		defaultPodSetLabel := "planner.ciena.io/default=enabled"

		// lookup default planners
		return s.lookupWithLabelSelector(parentCtx, defaultPodSetLabel,
			namespace, podset, scheduledPod, eligibleNodes)
	}

	return planners, nil
}

// Invoke is used to invoke all the podset planners.
func (planners PlannerList) Invoke(parentCtx context.Context,
	trigger *plannerv1alpha1.ScheduleTrigger) (map[string]string, error) {
	//nolint:prealloc
	var planRewards []*planReward

	var lastError error

	for _, plan := range planners {
		assignments, err := plan.BuildSchedulePlan(parentCtx)
		if err != nil {
			st := status.Convert(err)
			lastError = fmt.Errorf("%v:%w", st.Message(), ErrBuildingPlan)

			continue
		}

		planRewards = append(planRewards, computePlanReward(trigger, assignments))
	}

	if len(planRewards) == 0 {
		if lastError != nil {
			return nil, lastError
		}

		return nil, ErrBuildingPlan
	}

	if len(planRewards) == 1 {
		return planRewards[0].assignments, nil
	}

	sort.SliceStable(planRewards, func(i, j int) bool {
		return planRewards[i].reward > planRewards[j].reward
	})

	return planRewards[0].assignments, nil
}

func computePlanReward(_ *plannerv1alpha1.ScheduleTrigger, assignments map[string]string) *planReward {
	//nolint:gomnd
	return &planReward{assignments: assignments, reward: rand.Intn(100) + 1}
}

// BuildSchedulePlan is used to build a podset assignment plan by talking to the podset planner service.
func (p *Planner) BuildSchedulePlan(parentCtx context.Context) (map[string]string, error) {
	p.Log.V(1).Info("build-schedule-plan", "namespace", p.Namespace,
		"podset", p.Podset,
		"scheduledPod", p.ScheduledPod)

	dns := fmt.Sprintf("%s.%s.svc.cluster.local:7309", p.Service.Name, p.Service.Namespace)

	dctx, dcancel := context.WithTimeout(parentCtx, p.CallTimeout)
	defer dcancel()

	conn, err := grpc.DialContext(dctx, dns, p.DialOptions...)
	if err != nil {
		return nil, fmt.Errorf("error grpc dial: %w", err)
	}

	//nolint:errcheck
	defer conn.Close()

	client := planner.NewSchedulePlannerClient(conn)
	ctx, cancel := context.WithTimeout(parentCtx, p.CallTimeout)

	defer cancel()

	req := &planner.SchedulePlanRequest{
		Namespace:     p.Namespace,
		PodSet:        p.Podset,
		ScheduledPod:  p.ScheduledPod,
		EligibleNodes: p.EligibleNodes,
	}

	p.Log.V(1).Info("build-schedule-plan-request", "podset", p.Podset,
		"namespace", p.Namespace,
		"scheduledPod", p.ScheduledPod,
		"nodes", p.EligibleNodes)

	resp, err := client.BuildSchedulePlan(ctx, req)
	if err != nil {
		p.Log.Error(err, "build-schedule-plan-request-error")

		//nolint: wrapcheck
		return nil, err
	}

	if resp.Assignments != nil {
		p.Log.V(1).Info("build-schedule-plan-request", "assignments", resp.Assignments)
	}

	return resp.Assignments, nil
}
