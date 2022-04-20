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
	"fmt"
	"time"

	constraintv1alpha1 "github.com/ciena/turnbuckle/pkg/apis/constraint/v1alpha1"
	"github.com/go-logr/logr"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
)

// nolint:gochecknoglobals
var (
	scheme         = runtime.NewScheme()
	codecs         = serializer.NewCodecFactory(scheme)
	parameterCodec = runtime.NewParameterCodec(scheme)
)

// ConstraintPolicyClient defines methods supported by the constraint policy
// client.
// nolint:lll
type ConstraintPolicyClient interface {
	GetConstraintPolicyBinding(ctx context.Context, namespace, name string, opts v1.GetOptions) (*constraintv1alpha1.ConstraintPolicyBinding, error)
	GetConstraintPolicyOffer(ctx context.Context, namespace, name string, opts v1.GetOptions) (*constraintv1alpha1.ConstraintPolicyOffer, error)
	GetConstraintPolicy(ctx context.Context, namespace, name string, opts v1.GetOptions) (*constraintv1alpha1.ConstraintPolicy, error)
	ListConstraintPolicyBindings(ctx context.Context, namespace string, opts v1.ListOptions) (*constraintv1alpha1.ConstraintPolicyBindingList, error)
	ListConstraintPolicyOffers(ctx context.Context, namespace string, opts v1.ListOptions) (*constraintv1alpha1.ConstraintPolicyOfferList, error)
	ListConstraintPolicies(ctx context.Context, namespace string, opts v1.ListOptions) (*constraintv1alpha1.ConstraintPolicyList, error)
}

type constraintPolicyClient struct {
	client rest.Interface
	log    logr.Logger
}

// nolint:gochecknoinits
func init() {
	v1.AddToGroupVersion(scheme, constraintv1alpha1.GroupVersion)
	utilruntime.Must(constraintv1alpha1.AddToScheme(scheme))
}

// New creates and returns a new constraint policy client instance.
// nolint:ireturn
func New(config *rest.Config, log logr.Logger) (ConstraintPolicyClient, error) {
	client, err := newConstraintClientForConfig(config)
	if err != nil {
		return nil, err
	}

	return &constraintPolicyClient{client: client, log: log}, nil
}

func newConstraintClientForConfig(c *rest.Config) (*rest.RESTClient, error) {
	config := *c
	setConfigDefaults(&config)

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, fmt.Errorf("unable to create REST client: %w", err)
	}

	return client, nil
}

func setConfigDefaults(config *rest.Config) {
	gv := constraintv1alpha1.GroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = codecs.WithoutConversion()

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}
}

// nolint:lll
func (c *constraintPolicyClient) GetConstraintPolicyBinding(ctx context.Context, namespace, name string, opts v1.GetOptions) (result *constraintv1alpha1.ConstraintPolicyBinding, err error) {
	result = &constraintv1alpha1.ConstraintPolicyBinding{}

	err = c.client.Get().
		Namespace(namespace).
		Resource("constraintpolicybindings").
		Name(name).
		VersionedParams(&opts, parameterCodec).
		Do(ctx).
		Into(result)

	return
}

// nolint:lll
func (c *constraintPolicyClient) GetConstraintPolicyOffer(ctx context.Context, namespace, name string, opts v1.GetOptions) (result *constraintv1alpha1.ConstraintPolicyOffer, err error) {
	result = &constraintv1alpha1.ConstraintPolicyOffer{}

	err = c.client.Get().
		Namespace(namespace).
		Resource("constraintpolicyoffers").
		Name(name).
		VersionedParams(&opts, parameterCodec).
		Do(ctx).
		Into(result)

	return
}

// nolint:lll
func (c *constraintPolicyClient) GetConstraintPolicy(ctx context.Context, namespace, name string, opts v1.GetOptions) (result *constraintv1alpha1.ConstraintPolicy, err error) {
	result = &constraintv1alpha1.ConstraintPolicy{}

	err = c.client.Get().
		Namespace(namespace).
		Resource("constraintpolicies").
		Name(name).
		VersionedParams(&opts, parameterCodec).
		Do(ctx).
		Into(result)

	return
}

// nolint:dupl,gocritic,lll
func (c *constraintPolicyClient) ListConstraintPolicyBindings(ctx context.Context, namespace string, opts v1.ListOptions) (result *constraintv1alpha1.ConstraintPolicyBindingList, err error) {
	result = &constraintv1alpha1.ConstraintPolicyBindingList{}

	var timeout time.Duration

	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}

	err = c.client.Get().
		Namespace(namespace).
		Resource("constraintpolicybindings").
		VersionedParams(&opts, parameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)

	return
}

// nolint:dupl,gocritic,lll
func (c *constraintPolicyClient) ListConstraintPolicyOffers(ctx context.Context, namespace string, opts v1.ListOptions) (result *constraintv1alpha1.ConstraintPolicyOfferList, err error) {
	result = &constraintv1alpha1.ConstraintPolicyOfferList{}

	var timeout time.Duration

	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}

	err = c.client.Get().
		Namespace(namespace).
		Resource("constraintpolicyoffers").
		VersionedParams(&opts, parameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)

	return
}

// nolint:dupl,gocritic,lll
func (c *constraintPolicyClient) ListConstraintPolicies(ctx context.Context, namespace string, opts v1.ListOptions) (result *constraintv1alpha1.ConstraintPolicyList, err error) {
	result = &constraintv1alpha1.ConstraintPolicyList{}

	var timeout time.Duration

	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}

	err = c.client.Get().
		Namespace(namespace).
		Resource("constraintpolicies").
		VersionedParams(&opts, parameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)

	return
}
