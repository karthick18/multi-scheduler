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
	"time"

	constraintv1alpha1 "github.com/ciena/turnbuckle/pkg/apis/constraint/v1alpha1"
	"github.com/ciena/turnbuckle/pkg/apis/underlay"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
)

type nodeOffer struct {
	node    string
	cluster string
	id      string
	cost    int64
	expires time.Time
}

// UnderlayController defines the methods supported by the underlay controller
// instances.
// nolint:lll
type UnderlayController interface {
	Discover(eligibleNodes []string, peerNodes []string, rules []*constraintv1alpha1.ConstraintPolicyRule) ([]*nodeOffer, error)
	Allocate(pathID string) error
	Release(pathID string) error
}

type underlayController struct {
	Log         logr.Logger
	Service     corev1.Service
	CallTimeout time.Duration
	DialOptions []grpc.DialOption
}

// nolint:lll
func (c *underlayController) Discover(eligibleNodes []string, peerNodes []string, rules []*constraintv1alpha1.ConstraintPolicyRule) ([]*nodeOffer, error) {
	c.Log.V(1).Info("discover", "namespace", c.Service.Namespace, "name", c.Service.Name)

	dns := fmt.Sprintf("%s.%s.svc.cluster.local:9999", c.Service.Name, c.Service.Namespace)

	dctx, dcancel := context.WithTimeout(context.Background(), c.CallTimeout)
	defer dcancel()

	conn, err := grpc.DialContext(dctx, dns, c.DialOptions...)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to underlay controller: %w", err)
	}

	// nolint:errcheck
	defer conn.Close()

	client := underlay.NewUnderlayControllerClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), c.CallTimeout)
	defer cancel()

	var policyRules []*underlay.PolicyRule
	if len(rules) > 0 {
		policyRules = make([]*underlay.PolicyRule, len(rules))
	}

	// unused, so just send empty
	for i := range rules {
		policyRules[i] = &underlay.PolicyRule{}
	}

	req := underlay.DiscoverRequest{
		Rules:         policyRules,
		EligibleNodes: eligibleNodes,
		PeerNodes:     peerNodes,
	}

	resp, err := client.Discover(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("unable to discover nodes: %w", err)
	}

	if len(resp.Offers) == 0 {
		return nil, ErrNoNodesFound
	}

	nodeOffers := make([]*nodeOffer, len(resp.Offers))

	for i, offer := range resp.Offers {
		nodeOffers[i] = &nodeOffer{
			node:    offer.Node.Name,
			cluster: offer.Node.Cluster,
			cost:    offer.Cost,
			id:      offer.Id,
			expires: offer.Expires.AsTime(),
		}
	}

	return nodeOffers, nil
}

// nolint:dupl
func (c *underlayController) Allocate(pathID string) error {
	c.Log.V(1).Info("allocatepaths", "namespace", c.Service.Namespace, "name", c.Service.Name)
	dns := fmt.Sprintf("%s.%s.svc.cluster.local:9999", c.Service.Name, c.Service.Namespace)

	dctx, dcancel := context.WithTimeout(context.Background(), c.CallTimeout)
	defer dcancel()

	conn, err := grpc.DialContext(dctx, dns, c.DialOptions...)
	if err != nil {
		return fmt.Errorf("unable to connect to underlay controller: %w", err)
	}

	// nolint:errcheck
	defer conn.Close()

	client := underlay.NewUnderlayControllerClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), c.CallTimeout)
	defer cancel()

	req := underlay.AllocateRequest{Id: pathID}
	if _, err = client.Allocate(ctx, &req); err != nil {
		return fmt.Errorf("unable to allocate underlay: %w", err)
	}

	return nil
}

// nolint:dupl
func (c *underlayController) Release(pathID string) error {
	c.Log.V(1).Info("releasepaths", "namespace", c.Service.Namespace, "name", c.Service.Name)
	dns := fmt.Sprintf("%s.%s.svc.cluster.local:9999", c.Service.Name, c.Service.Namespace)

	dctx, dcancel := context.WithTimeout(context.Background(), c.CallTimeout)
	defer dcancel()

	conn, err := grpc.DialContext(dctx, dns, c.DialOptions...)
	if err != nil {
		return fmt.Errorf("unable to connect to underlay controller: %w", err)
	}

	// nolint:errcheck
	defer conn.Close()

	client := underlay.NewUnderlayControllerClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), c.CallTimeout)
	defer cancel()

	req := underlay.ReleaseRequest{Id: pathID}

	if _, err = client.Release(ctx, &req); err != nil {
		return fmt.Errorf("unable to release client connection: %w", err)
	}

	return nil
}
