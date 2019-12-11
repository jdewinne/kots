/*
Copyright 2019 Replicated, Inc..

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
// Code generated by client-gen. DO NOT EDIT.

package v1beta1

import (
	"time"

	v1beta1 "github.com/replicatedhq/kots/kotskinds/apis/kots/v1beta1"
	scheme "github.com/replicatedhq/kots/kotskinds/client/kotsclientset/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// AirgapsGetter has a method to return a AirgapInterface.
// A group's client should implement this interface.
type AirgapsGetter interface {
	Airgaps(namespace string) AirgapInterface
}

// AirgapInterface has methods to work with Airgap resources.
type AirgapInterface interface {
	Create(*v1beta1.Airgap) (*v1beta1.Airgap, error)
	Update(*v1beta1.Airgap) (*v1beta1.Airgap, error)
	UpdateStatus(*v1beta1.Airgap) (*v1beta1.Airgap, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1beta1.Airgap, error)
	List(opts v1.ListOptions) (*v1beta1.AirgapList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1beta1.Airgap, err error)
	AirgapExpansion
}

// airgaps implements AirgapInterface
type airgaps struct {
	client rest.Interface
	ns     string
}

// newAirgaps returns a Airgaps
func newAirgaps(c *KotsV1beta1Client, namespace string) *airgaps {
	return &airgaps{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the airgap, and returns the corresponding airgap object, and an error if there is any.
func (c *airgaps) Get(name string, options v1.GetOptions) (result *v1beta1.Airgap, err error) {
	result = &v1beta1.Airgap{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("airgaps").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Airgaps that match those selectors.
func (c *airgaps) List(opts v1.ListOptions) (result *v1beta1.AirgapList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1beta1.AirgapList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("airgaps").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested airgaps.
func (c *airgaps) Watch(opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("airgaps").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch()
}

// Create takes the representation of a airgap and creates it.  Returns the server's representation of the airgap, and an error, if there is any.
func (c *airgaps) Create(airgap *v1beta1.Airgap) (result *v1beta1.Airgap, err error) {
	result = &v1beta1.Airgap{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("airgaps").
		Body(airgap).
		Do().
		Into(result)
	return
}

// Update takes the representation of a airgap and updates it. Returns the server's representation of the airgap, and an error, if there is any.
func (c *airgaps) Update(airgap *v1beta1.Airgap) (result *v1beta1.Airgap, err error) {
	result = &v1beta1.Airgap{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("airgaps").
		Name(airgap.Name).
		Body(airgap).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *airgaps) UpdateStatus(airgap *v1beta1.Airgap) (result *v1beta1.Airgap, err error) {
	result = &v1beta1.Airgap{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("airgaps").
		Name(airgap.Name).
		SubResource("status").
		Body(airgap).
		Do().
		Into(result)
	return
}

// Delete takes name of the airgap and deletes it. Returns an error if one occurs.
func (c *airgaps) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("airgaps").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *airgaps) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	var timeout time.Duration
	if listOptions.TimeoutSeconds != nil {
		timeout = time.Duration(*listOptions.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("airgaps").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Timeout(timeout).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched airgap.
func (c *airgaps) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1beta1.Airgap, err error) {
	result = &v1beta1.Airgap{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("airgaps").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}