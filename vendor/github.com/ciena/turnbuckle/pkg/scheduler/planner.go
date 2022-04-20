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
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	constraint_policy_client "github.com/ciena/turnbuckle/internal/pkg/constraint-policy-client"
	constraintv1alpha1 "github.com/ciena/turnbuckle/pkg/apis/constraint/v1alpha1"
	"github.com/ciena/turnbuckle/pkg/types"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	kindPod = "Pod"
)

// ConstraintPolicySchedulerPlanner instance state for planner.
type ConstraintPolicySchedulerPlanner struct {
	options                ConstraintPolicySchedulerPlannerOptions
	clientset              kubernetes.Interface
	constraintPolicyClient constraint_policy_client.ConstraintPolicyClient
	log                    logr.Logger
	nodeLister             listersv1.NodeLister
	quit                   chan struct{}
	nodeQueue              chan *v1.Node
	podUpdateQueue         workqueue.RateLimitingInterface
	podToNodeMap           map[ktypes.NamespacedName]string
	constraintPolicyMutex  sync.Mutex
}

// ConstraintPolicySchedulerPlannerOptions options for the planner.
type ConstraintPolicySchedulerPlannerOptions struct {
	CallTimeout        time.Duration
	UpdateWorkerPeriod time.Duration
	NodeQueueSize      uint
	AddPodCallback     func(pod *v1.Pod)
	UpdatePodCallback  func(old, update *v1.Pod)
	DeletePodCallback  func(pod *v1.Pod)
}

// NodeAndCost tuple of a node and the cost of using that node.
type NodeAndCost struct {
	Node string
	Cost int64
}

type constraintPolicyOffer struct {
	offer         *constraintv1alpha1.ConstraintPolicyOffer
	peerToNodeMap map[ktypes.NamespacedName]string
	peerNodeNames []string
}

type workWrapper struct {
	work func() error
}

// NewPlanner creates a new planner with specified callbacks.
func NewPlanner(
	options ConstraintPolicySchedulerPlannerOptions,
	clientset kubernetes.Interface,
	constraintPolicyClient constraint_policy_client.ConstraintPolicyClient,
	log logr.Logger) *ConstraintPolicySchedulerPlanner {
	constraintPolicySchedulerPlanner := &ConstraintPolicySchedulerPlanner{
		options:                options,
		clientset:              clientset,
		constraintPolicyClient: constraintPolicyClient,
		log:                    log,
		nodeQueue:              make(chan *v1.Node, options.NodeQueueSize),
		podUpdateQueue:         workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		quit:                   make(chan struct{}),
		podToNodeMap:           make(map[ktypes.NamespacedName]string),
	}

	addFunc := func(obj interface{}) {
		pod, ok := obj.(*v1.Pod)
		if !ok {
			log.V(1).Info("this-is-not-a-pod")

			return
		}

		if options.AddPodCallback != nil {
			options.AddPodCallback(pod)
		}
	}

	updateFunc := func(oldObj interface{}, newObj interface{}) {
		oldPod, status := oldObj.(*v1.Pod)
		if !status {
			return
		}

		newPod, status := newObj.(*v1.Pod)
		if !status {
			return
		}

		constraintPolicySchedulerPlanner.handlePodUpdate(oldPod, newPod)

		if options.UpdatePodCallback != nil {
			options.UpdatePodCallback(oldPod, newPod)
		}
	}

	deleteFunc := func(obj interface{}) {
		pod, ok := obj.(*v1.Pod)
		if !ok {
			return
		}

		constraintPolicySchedulerPlanner.handlePodDelete(pod)

		if options.DeletePodCallback != nil {
			options.DeletePodCallback(pod)
		}
	}

	constraintPolicySchedulerPlanner.nodeLister = initInformers(
		clientset,
		log,
		constraintPolicySchedulerPlanner.quit,
		constraintPolicySchedulerPlanner.nodeQueue,
		addFunc,
		updateFunc,
		deleteFunc,
	)

	go constraintPolicySchedulerPlanner.listenForNodeEvents()
	go constraintPolicySchedulerPlanner.listenForPodUpdateEvents()

	return constraintPolicySchedulerPlanner
}

func getEligibleNodes(nodeLister listersv1.NodeLister) ([]*v1.Node, error) {
	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("unable to list nodes: %w", err)
	}

	var eligibleNodes, preferNoScheduleNodes []*v1.Node

	for _, node := range nodes {
		preferNoSchedule, noSchedule := false, false

		for i := range node.Spec.Taints {
			if node.Spec.Taints[i].Effect == v1.TaintEffectPreferNoSchedule {
				preferNoSchedule = true
			} else if node.Spec.Taints[i].Effect == v1.TaintEffectNoSchedule ||
				node.Spec.Taints[i].Effect == v1.TaintEffectNoExecute {
				noSchedule = true
			}
		}

		if !noSchedule {
			eligibleNodes = append(eligibleNodes, node)
		} else if preferNoSchedule {
			preferNoScheduleNodes = append(preferNoScheduleNodes, node)
		}
	}

	if len(eligibleNodes) == 0 {
		return preferNoScheduleNodes, nil
	}

	return eligibleNodes, nil
}

func initInformers(clientset kubernetes.Interface,
	log logr.Logger,
	quit chan struct{},
	nodeQueue chan *v1.Node,
	addFunc func(obj interface{}),
	updateFunc func(oldObj interface{}, newObj interface{}),
	deleteFunc func(obj interface{}),
) listersv1.NodeLister {
	factory := informers.NewSharedInformerFactory(clientset, 0)
	nodeInformer := factory.Core().V1().Nodes()
	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node, ok := obj.(*v1.Node)
			if !ok {
				log.V(1).Info("this-is-not-a-node")

				return
			}
			log.V(1).Info("new-node-added", "node", node.GetName())
			nodeQueue <- node
		},
	})

	podInformer := factory.Core().V1().Pods()
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    addFunc,
		UpdateFunc: updateFunc,
		DeleteFunc: deleteFunc,
	})

	factory.Start(quit)

	return nodeInformer.Lister()
}

// GetClientset returns the client set for the planner.
// nolint:ireturn
func (s *ConstraintPolicySchedulerPlanner) GetClientset() kubernetes.Interface {
	return s.clientset
}

func (s *ConstraintPolicySchedulerPlanner) getEligibleNodesAndNodeNames() ([]*v1.Node, []string, error) {
	nodeRefs, err := getEligibleNodes(s.nodeLister)
	if err != nil {
		return nil, nil, err
	}

	nodeNames := make([]string, len(nodeRefs))

	for i, n := range nodeRefs {
		nodeNames[i] = n.Name
	}

	return nodeRefs, nodeNames, nil
}

func (s *ConstraintPolicySchedulerPlanner) handlePodUpdate(oldPod *v1.Pod, newPod *v1.Pod) {
	s.constraintPolicyMutex.Lock()
	defer s.constraintPolicyMutex.Unlock()

	if oldPod.Status.Phase != v1.PodRunning && newPod.Status.Phase == v1.PodRunning {
		delete(s.podToNodeMap, ktypes.NamespacedName{Name: newPod.Name, Namespace: newPod.Namespace})
	} else if oldPod.GetDeletionTimestamp() == nil && newPod.GetDeletionTimestamp() != nil {
		if err := s.releaseUnderlayPath(newPod); err != nil {
			s.log.V(1).Info("handle-pod-update-enqueue-event-on-failure", "pod", newPod.Name)
			s.podUpdateQueue.AddRateLimited(newPod)
		}
	}
}

func (s *ConstraintPolicySchedulerPlanner) releaseUnderlayPath(pod *v1.Pod) error {
	finalizers := pod.GetFinalizers()
	if finalizers == nil {
		return nil
	}

	// check for finalizers if any to release underlay path
	finalizerPrefix := "constraint.ciena.com/remove-underlay_"
	underlayFinalizers := []string{}
	newFinalizers := []string{}

	for _, finalizer := range finalizers {
		if strings.HasPrefix(finalizer, finalizerPrefix) {
			underlayFinalizers = append(underlayFinalizers, finalizer)
		} else {
			newFinalizers = append(newFinalizers, finalizer)
		}
	}

	if len(underlayFinalizers) == 0 {
		return nil
	}

	// update the pod finalizer with the new set
	pod.ObjectMeta.Finalizers = newFinalizers

	if _, err := s.clientset.CoreV1().Pods(pod.Namespace).Update(
		context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		s.log.Error(err, "underlay-path-release-pod-update-failed", "pod",
			pod.Name)

		return fmt.Errorf("unable to list pods: %w", err)
	}

	s.log.V(1).Info("underlay-path-release-pod-update-success", "pod", pod.Name)

	underlayController, err := s.lookupUnderlayController(context.Background())
	if err != nil {
		s.log.Error(err, "underlay-lookup-failed")

		return err
	}

	for _, finalizer := range underlayFinalizers {
		pathID := finalizer[len(finalizerPrefix):]
		s.log.V(1).Info("underlay-path-release", "pod", pod.Name, "path", pathID)

		if err := underlayController.Release(pathID); err != nil {
			s.log.Error(err, "underlay-path-release-failed", "path", pathID)
		} else {
			s.log.V(1).Info("underlay-path-release-success", "path", pathID)
		}
	}

	return nil
}

func (s *ConstraintPolicySchedulerPlanner) handlePodDelete(pod *v1.Pod) {
	s.constraintPolicyMutex.Lock()
	defer s.constraintPolicyMutex.Unlock()
	delete(s.podToNodeMap, ktypes.NamespacedName{Name: pod.Name, Namespace: pod.Namespace})
}

// ParseDuration parses a list of durations.
func ParseDuration(durationStrings ...string) ([]time.Duration, error) {
	durations := make([]time.Duration, len(durationStrings))

	for index, ds := range durationStrings {
		duration, err := time.ParseDuration(ds)
		if err != nil {
			return nil, fmt.Errorf("unble to parse '%s': %w", ds, err)
		}

		durations[index] = duration
	}

	return durations, nil
}

// FindNodeLister looks up the node lister based on the node name.
func (s *ConstraintPolicySchedulerPlanner) FindNodeLister(node string) (*v1.Node, error) {
	nodes, err := getEligibleNodes(s.nodeLister)
	if err != nil {
		return nil, err
	}

	for _, n := range nodes {
		if n.Name == node {
			return n, nil
		}
	}

	return nil, fmt.Errorf("could not find node lister instance for node %s: %w",
		node, ErrNotFound)
}

// Stop stops the constraint polucy scheduler planner.
func (s *ConstraintPolicySchedulerPlanner) Stop() {
	close(s.quit)
}

func matchesLabelSelector(labelSelector *metav1.LabelSelector, pod *v1.Pod) (bool, error) {
	set, err := metav1.LabelSelectorAsMap(labelSelector)
	if err != nil {
		return false, fmt.Errorf("error creating label selector: %w", err)
	}

	return labels.Set(set).AsSelector().Matches(labels.Set(pod.Labels)), nil
}

func (s *ConstraintPolicySchedulerPlanner) getPeerEndpoints(
	ctx context.Context,
	endpointLabels labels.Set) (map[ktypes.NamespacedName]string, error,
) {
	endpoints, err := s.clientset.CoreV1().Endpoints("").List(ctx,
		metav1.ListOptions{LabelSelector: endpointLabels.String()})
	if err != nil {
		return nil, fmt.Errorf("error listing endpoints: %w", err)
	}

	// this is a 1:1 as endpoing name is stored for network telemetry info
	endpointNodeMap := make(map[ktypes.NamespacedName]string)

	for i := range endpoints.Items {
		endpointNodeMap[ktypes.NamespacedName{
			Name:      endpoints.Items[i].Name,
			Namespace: endpoints.Items[i].Namespace,
		}] = endpoints.Items[i].Name
	}

	return endpointNodeMap, nil
}

func (s *ConstraintPolicySchedulerPlanner) getPeerPods(
	ctx context.Context,
	podLabels labels.Set,
) (map[ktypes.NamespacedName]string, error) {
	pods, err := s.clientset.CoreV1().Pods("").List(ctx,
		metav1.ListOptions{LabelSelector: podLabels.String()})
	if err != nil {
		return nil, fmt.Errorf("error listing pods: %w", err)
	}

	podNodeMap := make(map[ktypes.NamespacedName]string)

	for index := range pods.Items {
		if pods.Items[index].Status.Phase == v1.PodFailed || pods.Items[index].DeletionTimestamp != nil {
			continue
		}

		var podNodeName string

		if nodeName, err := s.GetNodeName(&pods.Items[index]); err == nil {
			podNodeName = nodeName
		} else {
			podNodeName = ""
		}

		podNodeMap[ktypes.NamespacedName{Name: pods.Items[index].Name, Namespace: pods.Items[index].Namespace}] = podNodeName
	}

	return podNodeMap, nil
}

func (s *ConstraintPolicySchedulerPlanner) getPeers(
	ctx context.Context,
	selector *constraintv1alpha1.ConstraintPolicyOfferTarget,
) (map[ktypes.NamespacedName]string, error) {
	if selector.Kind == kindPod && selector.LabelSelector != nil {
		set, err := metav1.LabelSelectorAsMap(selector.LabelSelector)
		if err != nil {
			s.log.Error(err, "error-getting-label-selector")

			return nil, fmt.Errorf("error creating label selector: %w", err)
		}

		return s.getPeerPods(ctx, labels.Set(set))
	}

	if selector.Kind == "Endpoint" && selector.LabelSelector != nil {
		set, err := metav1.LabelSelectorAsMap(selector.LabelSelector)
		if err != nil {
			s.log.Error(err, "error-getting-label-selector-for-endpoints")

			return nil, fmt.Errorf("error creating endpoint label selector: %w",
				err)
		}

		return s.getPeerEndpoints(ctx, labels.Set(set))
	}

	return nil, ErrNotFound
}

func getPeerNodeNames(peerToNodeMap map[ktypes.NamespacedName]string) []string {
	nodeNames := make([]string, 0, len(peerToNodeMap))
	visitedMap := make(map[string]struct{})

	for _, node := range peerToNodeMap {
		if node == "" {
			continue
		}

		if _, ok := visitedMap[node]; !ok {
			nodeNames = append(nodeNames, node)
			visitedMap[node] = struct{}{}
		}
	}

	return nodeNames
}

func mergePeers(peer1, peer2 map[ktypes.NamespacedName]string) map[ktypes.NamespacedName]string {
	result := make(map[ktypes.NamespacedName]string)

	for k, v := range peer1 {
		result[k] = v
	}

	for k, v := range peer2 {
		result[k] = v
	}

	return result
}

func (s *ConstraintPolicySchedulerPlanner) processPolicyOfferTargets(pod *v1.Pod,
	offer *constraintv1alpha1.ConstraintPolicyOffer) ([]*constraintv1alpha1.ConstraintPolicyOfferTarget, bool,
) {
	var matched bool

	peers := []*constraintv1alpha1.ConstraintPolicyOfferTarget{}

	for _, target := range offer.Spec.Targets {
		var targetMatch bool

		if target.Kind == kindPod && target.LabelSelector != nil {
			match, err := matchesLabelSelector(target.LabelSelector, pod)
			if err != nil {
				s.log.Error(err, "error-matching-source-label-selector", "offer", offer.Name)

				continue
			}

			targetMatch = match
		}

		if !targetMatch {
			peers = append(peers, target)
		} else {
			matched = targetMatch
		}
	}

	return peers, matched
}

func (s *ConstraintPolicySchedulerPlanner) getPolicyOffers(
	parentCtx context.Context,
	pod *v1.Pod) ([]*constraintPolicyOffer, error,
) {
	offers, err := s.constraintPolicyClient.ListConstraintPolicyOffers(parentCtx, pod.Namespace, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing policy offers: %w", err)
	}

	var offerList []*constraintPolicyOffer

	for _, offer := range offers.Items {
		var peerToNodeMap map[ktypes.NamespacedName]string

		var peerNodeNames []string

		peers, matched := s.processPolicyOfferTargets(pod, offer)

		if matched {
			for _, peer := range peers {
				if peer.LabelSelector == nil {
					continue
				}

				ctx, cancel := context.WithTimeout(parentCtx, s.options.CallTimeout)

				if peerMap, err := s.getPeers(ctx, peer); err == nil {
					peerToNodeMap = mergePeers(peerMap, peerToNodeMap)
				}

				cancel()
			}

			peerNodeNames = getPeerNodeNames(peerToNodeMap)

			offerList = append(offerList, &constraintPolicyOffer{
				offer:         offer,
				peerToNodeMap: peerToNodeMap,
				peerNodeNames: peerNodeNames,
			})
		}
	}

	if len(offerList) == 0 {
		return nil, ErrNoOffers
	}

	return offerList, nil
}

func (s *ConstraintPolicySchedulerPlanner) lookupUnderlayController(ctx context.Context) (UnderlayController, error) {
	svcs, err := s.clientset.CoreV1().Services("").List(ctx,
		metav1.ListOptions{LabelSelector: "constraint.ciena.com/underlay-controller"})
	if err != nil {
		return nil, fmt.Errorf("error listing underlay controller: %w", err)
	}

	if len(svcs.Items) == 0 {
		return nil, ErrNotFound
	}

	return &underlayController{
		Log:         s.log.WithName("underlay-controller"),
		Service:     svcs.Items[0],
		CallTimeout: s.options.CallTimeout,
		DialOptions: []grpc.DialOption{
			grpc.WithInsecure(),
		},
	}, nil
}

func (s *ConstraintPolicySchedulerPlanner) lookupRuleProvider(ctx context.Context,
	name, namespace string) (RuleProvider, error) {
	svcs, err := s.clientset.CoreV1().Services(namespace).List(ctx,
		metav1.ListOptions{LabelSelector: fmt.Sprintf("constraint.ciena.com/provider-%s", name)})
	if err != nil {
		return nil, fmt.Errorf("error listing rule provider: %w", err)
	}

	if len(svcs.Items) == 0 {
		return nil, ErrNotFound
	}

	return &ruleProvider{
		Log:         s.log.WithName("rule-provider").WithName(name),
		ProviderFor: name,
		Service:     svcs.Items[0],
		CallTimeout: s.options.CallTimeout,
		DialOptions: []grpc.DialOption{
			grpc.WithInsecure(),
		},
	}, nil
}

func mergeOfferCost(m1 map[string]int64, m2 map[string]int64) map[string]int64 {
	result := make(map[string]int64)

	for k, v := range m1 {
		if v2, ok := m2[k]; ok {
			// nolint:gomnd
			result[k] = (v + v2) / 2
		}
	}

	return result
}

func mergeNodeCost(m1 map[string][]int64, m2 map[string][]int64) map[string][]int64 {
	result := make(map[string][]int64)

	for k, v1 := range m1 {
		if v2, ok := m2[k]; ok {
			// concat m1[k] and m2[k] values on intersect
			result[k] = append(result[k], v1...)
			result[k] = append(result[k], v2...)
		}
	}

	return result
}

func mergeNodeAllocationCost(m1 map[NodeAndCost]string, m2 map[NodeAndCost]string) map[NodeAndCost]string {
	result := make(map[NodeAndCost]string)

	for nc := range m1 {
		if m2Id, ok := m2[nc]; ok {
			// we can use both as id is in both the lists
			result[nc] = m2Id
		}
	}

	return result
}

func mergeRules(
	existingRules []*constraintv1alpha1.ConstraintPolicyRule,
	newRules []*constraintv1alpha1.ConstraintPolicyRule,
) []*constraintv1alpha1.ConstraintPolicyRule {
	presentMap := make(map[string]struct{})

	for _, r := range existingRules {
		presentMap[r.Name] = struct{}{}
	}

	for _, r := range newRules {
		if _, ok := presentMap[r.Name]; !ok {
			existingRules = append(existingRules, r)
		}
	}

	return existingRules
}

func getAggregate(values []int64) int64 {
	var sum int64

	for i := range values {
		sum += values[i]
	}

	if len(values) > 1 {
		sum /= int64(len(values))
	}

	return sum
}

func filterOutInfiniteCost(nodeAndCost []NodeAndCost) []NodeAndCost {
	out := []NodeAndCost{}

	for _, nc := range nodeAndCost {
		if nc.Cost >= 0 {
			out = append(out, nc)
		}
	}

	return out
}

func (s *ConstraintPolicySchedulerPlanner) getEndpointCost(
	ctx context.Context,
	src *types.Reference,
	policyRules []*constraintv1alpha1.ConstraintPolicyRule,
	eligibleNodes []string,
	peerNodeNames []string,
) (map[string]int64, []*constraintv1alpha1.ConstraintPolicyRule, error) {
	ruleToCostMap := make(map[string][]NodeAndCost)
	matchedRules := []*constraintv1alpha1.ConstraintPolicyRule{}

	for _, rule := range policyRules {
		provider, err := s.lookupRuleProvider(ctx, rule.Name, "")
		if err != nil {
			s.log.Error(err, "error-looking-up-rule-provider", "rule", rule.Name)

			continue
		}

		if nodeAndCost, err := provider.EndpointCost(src, eligibleNodes, peerNodeNames,
			rule.Request, rule.Limit); err != nil {
			s.log.Error(err, "error-getting-endpoint-cost", "rule", rule.Name)
		} else {
			nodeAndCost = filterOutInfiniteCost(nodeAndCost)
			ruleToCostMap[rule.Name] = nodeAndCost

			if len(nodeAndCost) > 0 {
				matchedRules = append(matchedRules, rule)
			}
		}
	}

	var mergedNodeCostMap map[string][]int64

	for _, nodeAndCost := range ruleToCostMap {
		nodeCostMap := make(map[string][]int64)

		for _, nc := range nodeAndCost {
			nodeCostMap[nc.Node] = append(nodeCostMap[nc.Node], nc.Cost)
		}

		if mergedNodeCostMap != nil {
			mergedNodeCostMap = mergeNodeCost(mergedNodeCostMap, nodeCostMap)
		} else {
			mergedNodeCostMap = nodeCostMap
		}
	}

	if mergedNodeCostMap == nil {
		return nil, nil, ErrNoCost
	}

	// for each node compute the aggregated cost now from the intersects
	mergedNodeAggregateCostMap := make(map[string]int64)

	for node, values := range mergedNodeCostMap {
		mergedNodeAggregateCostMap[node] = getAggregate(values)
	}

	return mergedNodeAggregateCostMap, matchedRules, nil
}

func (s *ConstraintPolicySchedulerPlanner) getUnderlayCost(
	ctx context.Context,
	eligibleNodes []string,
	peerNodeNames []string,
	rules []*constraintv1alpha1.ConstraintPolicyRule,
) (map[string]int64, map[NodeAndCost]string, error) {
	// this will make a grpc to underlay and get the cost
	underlayController, err := s.lookupUnderlayController(ctx)
	if err != nil {
		s.log.Error(err, "underlay-controller-lookup-failed")

		return nil, nil, fmt.Errorf("unable to locate underlay controller: %w",
			err)
	}

	nodeOffers, err := underlayController.Discover(eligibleNodes, peerNodeNames, rules)
	if err != nil {
		s.log.Error(err, "underlay-controller-path-discovery-failed")

		return nil, nil,
			fmt.Errorf("failure calling discovery on underlay controller: %w", err)
	}

	nodeCostMap := make(map[string]int64)
	nodeAllocationPathMap := make(map[NodeAndCost]string)

	for _, offer := range nodeOffers {
		nodeCostMap[offer.node] = offer.cost
		nodeAllocationPathMap[NodeAndCost{Node: offer.node, Cost: offer.cost}] = offer.id
	}

	return nodeCostMap, nodeAllocationPathMap, nil
}

func (s *ConstraintPolicySchedulerPlanner) getUnderlayCostForOffers(
	parentCtx context.Context,
	matchingOffers []*constraintPolicyOffer,
	eligibleNodes []string,
	offerToRulesMap map[string][]*constraintv1alpha1.ConstraintPolicyRule,
) (map[string]int64, map[NodeAndCost]string, error) {
	var offerCostMap map[string]int64

	var nodeAllocationPathMap map[NodeAndCost]string

	for _, matchingOffer := range matchingOffers {
		rules, ok := offerToRulesMap[matchingOffer.offer.Name]
		if !ok {
			continue
		}

		if len(rules) == 0 {
			s.log.V(1).Info("get-underlay-cost-no-rules-found", "offer", matchingOffer.offer.Name)

			continue
		}

		var peerNodeNames []string

		if len(matchingOffer.peerNodeNames) == 0 {
			peerNodeNames = eligibleNodes
		} else {
			peerNodeNames = matchingOffer.peerNodeNames
		}

		ctx, cancel := context.WithTimeout(parentCtx, s.options.CallTimeout)

		nodeCostMap, allocationPathMap, err := s.getUnderlayCost(
			ctx, eligibleNodes, peerNodeNames, rules)

		cancel()

		if err != nil {
			s.log.Error(err, "error-getting-underlay-cost", "offer", matchingOffer.offer.Name)

			continue
		}

		if offerCostMap != nil {
			offerCostMap = mergeOfferCost(offerCostMap, nodeCostMap)
		} else {
			offerCostMap = nodeCostMap
		}

		if nodeAllocationPathMap != nil {
			nodeAllocationPathMap = mergeNodeAllocationCost(nodeAllocationPathMap, allocationPathMap)
		} else {
			nodeAllocationPathMap = allocationPathMap
		}
	}

	if len(offerCostMap) == 0 {
		return nil, nil, ErrNoCost
	}

	return offerCostMap, nodeAllocationPathMap, nil
}

func (s *ConstraintPolicySchedulerPlanner) getNodeWithBestCost(
	nodeCostMap map[string]int64, applyFilter func(string) bool) (NodeAndCost, error) {
	nodeCostList := make([]NodeAndCost, 0, len(nodeCostMap))
	for n, c := range nodeCostMap {
		nodeCostList = append(nodeCostList, NodeAndCost{Node: n, Cost: c})
	}

	if len(nodeCostList) == 0 {
		return NodeAndCost{}, ErrNoNodesFound
	}

	// sort the list based on cost
	sort.Slice(nodeCostList, func(i, j int) bool {
		return nodeCostList[i].Cost < nodeCostList[j].Cost
	})

	// filter out nodes that don't satisfy the filter
	for _, nodeAndCost := range nodeCostList {
		if applyFilter != nil && !applyFilter(nodeAndCost.Node) {
			continue
		}

		return nodeAndCost, nil
	}

	return NodeAndCost{}, ErrNoNodesFound
}

func (s *ConstraintPolicySchedulerPlanner) getPodCandidateNodes(parentCtx context.Context, pod *v1.Pod,
	eligibleNodes []string, offers []*constraintPolicyOffer) (map[string]int64, map[NodeAndCost]string, error) {
	var offerCostMap map[string]int64

	offerToRulesMap := make(map[string][]*constraintv1alpha1.ConstraintPolicyRule)
	nodeAllocationPathMap := make(map[NodeAndCost]string)

	for _, matchingOffer := range offers {
		var policyRules []*constraintv1alpha1.ConstraintPolicyRule

		for _, policyName := range matchingOffer.offer.Spec.Policies {
			ctx, cancel := context.WithTimeout(parentCtx, s.options.CallTimeout)

			policy, err := s.constraintPolicyClient.GetConstraintPolicy(ctx,
				matchingOffer.offer.Namespace, string(policyName), metav1.GetOptions{})

			cancel()

			if err != nil {
				s.log.Error(err, "error-getting-policy", "policy", string(policyName))

				continue
			}

			policyRules = mergeRules(policyRules, policy.Spec.Rules)
		}

		ctx, cancel := context.WithTimeout(parentCtx, s.options.CallTimeout)

		nodeCostMap, matchedRules, err := s.getEndpointCost(ctx,
			&types.Reference{Name: pod.Name, Kind: kindPod},
			policyRules, eligibleNodes, matchingOffer.peerNodeNames)

		cancel()

		if err != nil {
			s.log.Error(err, "error-getting-endpoint-cost", "offer", matchingOffer.offer.Name)

			continue
		}

		offerToRulesMap[matchingOffer.offer.Name] = matchedRules

		if offerCostMap != nil {
			offerCostMap = mergeOfferCost(offerCostMap, nodeCostMap)
		} else {
			offerCostMap = nodeCostMap
		}
	}

	if offerCostMap == nil {
		return nil, nil, fmt.Errorf("no offers were matched for pod %s: %w",
			pod.Name, ErrNoOffers)
	}

	if len(offerCostMap) == 0 {
		// if no nodes were found from ruleprovider, we go to the underlay
		var err error

		offerCostMap, nodeAllocationPathMap, err = s.getUnderlayCostForOffers(
			parentCtx, offers, eligibleNodes, offerToRulesMap)
		if err != nil {
			s.log.Error(err, "get-underlay-cost-for-offers-failed")

			return nil, nil, fmt.Errorf("unable to get underlay offers: %w", err)
		}
	}

	return offerCostMap, nodeAllocationPathMap, nil
}

// Start runs the constraint policy planner in go routines.
func (s *ConstraintPolicySchedulerPlanner) Start() {
	go s.listenForNodeEvents()
	go s.listenForPodUpdateEvents()
}

func (s *ConstraintPolicySchedulerPlanner) listenForNodeEvents() {
	for {
		select {
		case <-s.nodeQueue:
			// nothing to do for now

		case <-s.quit:
			return
		}
	}
}

// called with constraintpolicymutex held.
func (s *ConstraintPolicySchedulerPlanner) getPodNode(pod *v1.Pod) (string, error) {
	node, ok := s.podToNodeMap[ktypes.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}]
	if !ok {
		return "", fmt.Errorf("cannot find pod %s node: %w", pod.Name,
			ErrPodNotAssigned)
	}

	return node, nil
}

// called with constraintpolicymutex held.
func (s *ConstraintPolicySchedulerPlanner) setPodNode(pod *v1.Pod, nodeName string) {
	s.podToNodeMap[ktypes.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}] = nodeName
}

// FindFitRandom selects a randome node from the given or eligible nodes.
func (s *ConstraintPolicySchedulerPlanner) FindFitRandom(pod *v1.Pod, nodes []*v1.Node) (*v1.Node, error) {
	var err error

	selectFrom := nodes
	if len(selectFrom) == 0 {
		selectFrom, _, err = s.getEligibleNodesAndNodeNames()
		if err != nil {
			return nil, fmt.Errorf("unable to fetch eligible nodes and names: %w", err)
		}
	}

	return selectFrom[rand.Intn(len(selectFrom))], nil
}

// GetNodeName look up for the node in the lister cache if pod host ip is not
// set.
func (s *ConstraintPolicySchedulerPlanner) GetNodeName(pod *v1.Pod) (string, error) {
	if pod.Status.HostIP == "" {
		return s.getPodNode(pod)
	}

	nodes, err := getEligibleNodes(s.nodeLister)
	if err != nil {
		return "", err
	}

	for _, node := range nodes {
		for i := range node.Status.Addresses {
			if node.Status.Addresses[i].Address == pod.Status.HostIP {
				return node.Name, nil
			}
		}
	}

	return "", fmt.Errorf("pod ip %s not found in node lister cache: %w",
		pod.Status.HostIP, ErrNotFound)
}

// ToNodeName lookup the node name for a given endpoint reference.
func (s *ConstraintPolicySchedulerPlanner) ToNodeName(endpoint *types.Reference) (string, error) {
	if endpoint.Kind != kindPod {
		return endpoint.Name, nil
	}

	pods, err := s.clientset.CoreV1().Pods(endpoint.Namespace).List(context.Background(),
		metav1.ListOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("unable to list pods in namespace %s: %w",
			endpoint.Namespace, err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]

		if pod.Status.Phase == v1.PodFailed || pod.DeletionTimestamp != nil {
			continue
		}

		if pod.Name == endpoint.Name {
			// nolint:govet
			if nodeName, err := s.GetNodeName(pod); err == nil {
				return nodeName, nil
			}

			return "", fmt.Errorf("unable to get node name for pod %s: %w",
				pod.Name, err)
		}
	}

	return "", fmt.Errorf("unable to find node name for pod %s: %w",
		endpoint.Name, ErrNodeNameNotFound)
}

func (s *ConstraintPolicySchedulerPlanner) processUpdateEvents() bool {
	// get blocks till there is an item
	item, quit := s.podUpdateQueue.Get()
	if quit {
		return false
	}

	defer s.podUpdateQueue.Done(item)

	s.processUpdate(item)

	return true
}

// AddRateLimited add work function to rate limited queue.
func (s *ConstraintPolicySchedulerPlanner) AddRateLimited(work func() error) {
	s.podUpdateQueue.AddRateLimited(&workWrapper{work: work})
}

func (s *ConstraintPolicySchedulerPlanner) processUpdate(item interface{}) {
	forgetItem := true

	defer func() {
		if forgetItem {
			s.podUpdateQueue.Forget(item)
		}
	}()

	var data *v1.Pod

	// nolint:forcetypeassert
	switch itemType := item.(type) {
	case *v1.Pod:
		data = item.(*v1.Pod)
	case *workWrapper:
		workWrapped := item.(*workWrapper)
		if err := workWrapped.work(); err != nil {
			forgetItem = false

			s.log.V(1).Info("pod-update-work-failed", "numrequeues", s.podUpdateQueue.NumRequeues(item))

			s.podUpdateQueue.AddRateLimited(item)

			return
		}

		return
	default:
		s.log.V(1).Info("pod-update-item-not-a-pod-or-a-function", "type", itemType)

		return
	}

	// get the latest version of the item
	pod, err := s.clientset.CoreV1().Pods(data.Namespace).Get(context.Background(), data.Name, metav1.GetOptions{})
	if err != nil {
		return
	}

	if pod.GetDeletionTimestamp() == nil {
		s.log.V(1).Info("pod-update-no-deletion-timestamp", "pod", pod.Name)

		return
	}

	if pod.GetFinalizers() == nil {
		s.log.V(1).Info("pod-update-no-finalizers-found", "pod", pod.Name)
	}

	// try to release the underlay path.
	// requeue the update back on failure
	err = s.releaseUnderlayPath(pod)
	if err != nil {
		forgetItem = false

		s.log.V(1).Info("pod-update-release-underlay-path-failed", "pod",
			pod.Name, "numrequeues", s.podUpdateQueue.NumRequeues(item))
		s.podUpdateQueue.AddRateLimited(item)

		return
	}

	s.log.V(1).Info("pod-update-release-underlay-path-success", "pod", pod.Name)
}

func (s *ConstraintPolicySchedulerPlanner) listenForPodUpdateEvents() {
	defer s.podUpdateQueue.ShutDown()

	go wait.Until(s.updateWorker, s.options.UpdateWorkerPeriod, s.quit)

	<-s.quit
}

func (s *ConstraintPolicySchedulerPlanner) updateWorker() {
	for s.processUpdateEvents() {
	}
}

func (s *ConstraintPolicySchedulerPlanner) setPodFinalizer(ctx context.Context, pod *v1.Pod, pathID string) error {
	podFinalizer := "constraint.ciena.com/remove-underlay_" + pathID

	for _, finalizer := range pod.ObjectMeta.Finalizers {
		if finalizer == podFinalizer {
			// already exists
			return nil
		}
	}

	pod.ObjectMeta.Finalizers = append(pod.ObjectMeta.Finalizers, podFinalizer)
	if _, err := s.clientset.CoreV1().Pods(pod.Namespace).
		Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("error updating pod: %w", err)
	}

	return nil
}

func (s *ConstraintPolicySchedulerPlanner) podFitsNode(pod *v1.Pod, nodename string) bool {
	return true
}

func (s *ConstraintPolicySchedulerPlanner) findFit(
	parentCtx context.Context,
	pod *v1.Pod,
	eligibleNodes []*v1.Node,
	eligibleNodeNames []string) (*v1.Node, error) {
	if len(eligibleNodes) == 0 {
		s.log.V(1).Info("no-eligible-nodes-found", "pod", pod.Name)

		return nil, ErrNoNodesFound
	}

	offers, err := s.getPolicyOffers(parentCtx, pod)
	if err != nil {
		return nil, err
	}

	nodeCostMap, nodeAllocationPathMap, err := s.getPodCandidateNodes(parentCtx, pod, eligibleNodeNames, offers)
	if err != nil {
		s.log.Error(err, "nodes-not-found", "pod", pod.Name)

		return nil, err
	}

	s.log.V(1).Info("pod-node-assignment", "pod", pod.Name, "node-candidates", nodeCostMap)

	// find if the node was assigned to the neighbor pod
	matchedNode, err := s.getNodeWithBestCost(nodeCostMap, func(nodeName string) bool {
		return s.podFitsNode(pod, nodeName)
	})
	if err != nil {
		s.log.Error(err, "eligible-nodes-not-found", "pod", pod.Name)

		return nil, err
	}

	s.log.V(1).Info("pod-node-assignment", "pod", pod.Name, "node", matchedNode.Node, "cost", matchedNode.Cost)

	nodeInstance, err := s.FindNodeLister(matchedNode.Node)
	if err != nil {
		s.log.V(1).Info("node-instance-not-found-in-lister-cache", "node", matchedNode.Node)
		s.log.V(1).Info("random-assignment", "pod", pod.Name)

		return s.FindFitRandom(pod, eligibleNodes)
	}

	s.setPodNode(pod, nodeInstance.Name)

	// we check if we have to allocate a path to the underlay for this node
	underlayPathID, ok := nodeAllocationPathMap[matchedNode]
	if !ok {
		return nodeInstance, nil
	}

	ctx, cancel := context.WithTimeout(parentCtx, s.options.CallTimeout)

	underlayController, err := s.lookupUnderlayController(ctx)

	cancel()

	if err != nil {
		s.log.Error(err, "underlay-controller-lookup-failed", "for-node-and-costr", matchedNode)

		return nodeInstance, nil
	}

	err = underlayController.Allocate(underlayPathID)
	if err != nil {
		s.log.V(1).Info("underlay-controller-allocate-failed", "path-id", underlayPathID)
	} else {
		s.log.V(1).Info("underlay-controller-allocate-success", "path-id", underlayPathID)
	}

	ctx, cancel = context.WithTimeout(parentCtx, s.options.CallTimeout)

	err = s.setPodFinalizer(ctx, pod, underlayPathID)

	cancel()

	if err != nil {
		s.log.Error(err, "set-pod-finalizer-failed", "pod", pod.Name, "path", underlayPathID)
	} else {
		s.log.V(1).Info("set-pod-finalizer-success", "pod", pod.Name, "path", underlayPathID)
	}

	return nodeInstance, nil
}

// FindBestNode scheduler function to find the best node for the pod.
func (s *ConstraintPolicySchedulerPlanner) FindBestNode(
	ctx context.Context,
	pod *v1.Pod,
	feasibleNodes []*v1.Node) (*v1.Node, error,
) {
	var nodeNames []string

	var err error

	if len(feasibleNodes) == 0 {
		if feasibleNodes, nodeNames, err = s.getEligibleNodesAndNodeNames(); err != nil {
			return nil, err
		}
	} else {
		nodeNames = make([]string, len(feasibleNodes))
		for i, n := range feasibleNodes {
			nodeNames[i] = n.Name
		}
	}

	s.log.V(1).Info("find-best-node", "pod", pod.Name)

	s.constraintPolicyMutex.Lock()
	defer s.constraintPolicyMutex.Unlock()

	return s.findFit(ctx, pod, feasibleNodes, nodeNames)
}
