// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package restmapper

import (
	"strings"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StaticRESTMapper provides GVK to GVR mappings without querying a cluster.
type StaticRESTMapper struct {
	mappings map[schema.GroupVersionKind]meta.RESTMapping
}

// NewStaticRESTMapperFromCRDs creates a REST mapper from a list of CRDs.
func NewStaticRESTMapperFromCRDs(crds []*extv1.CustomResourceDefinition) meta.RESTMapper {
	mapper := &StaticRESTMapper{
		mappings: make(map[schema.GroupVersionKind]meta.RESTMapping),
	}

	for _, crd := range crds {
		group := crd.Spec.Group
		kind := crd.Spec.Names.Kind
		plural := crd.Spec.Names.Plural

		// Determine scope
		var scope meta.RESTScope
		if crd.Spec.Scope == extv1.NamespaceScoped {
			scope = meta.RESTScopeNamespace
		} else {
			scope = meta.RESTScopeRoot
		}

		// Create mapping for each version
		for _, version := range crd.Spec.Versions {
			gvk := schema.GroupVersionKind{
				Group:   group,
				Version: version.Name,
				Kind:    kind,
			}

			gvr := schema.GroupVersionResource{
				Group:    group,
				Version:  version.Name,
				Resource: plural,
			}

			mapper.mappings[gvk] = meta.RESTMapping{
				Resource:         gvr,
				GroupVersionKind: gvk,
				Scope:            scope,
			}
		}
	}

	return mapper
}

// NewEmptyRESTMapper creates an empty REST mapper.
func NewEmptyRESTMapper() meta.RESTMapper {
	return &StaticRESTMapper{
		mappings: make(map[schema.GroupVersionKind]meta.RESTMapping),
	}
}

// NewMultiRESTMapper chains multiple REST mappers with first-match semantics.
func NewMultiRESTMapper(mappers ...meta.RESTMapper) meta.RESTMapper {
	return meta.MultiRESTMapper(mappers)
}

// RESTMapping implements meta.RESTMapper.
func (m *StaticRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	// Try each version in order
	for _, version := range versions {
		gvk := schema.GroupVersionKind{
			Group:   gk.Group,
			Version: version,
			Kind:    gk.Kind,
		}
		if mapping, ok := m.mappings[gvk]; ok {
			return &mapping, nil
		}
	}

	// If no versions specified or none found, try to find any version
	if len(versions) == 0 {
		for k, v := range m.mappings {
			if k.Group == gk.Group && k.Kind == gk.Kind {
				mapping := v
				return &mapping, nil
			}
		}
	}

	return nil, &meta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{
		Group:    gk.Group,
		Resource: strings.ToLower(gk.Kind),
	}}
}

// RESTMappings implements meta.RESTMapper.
func (m *StaticRESTMapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error) {
	mapping, err := m.RESTMapping(gk, versions...)
	if err != nil {
		return nil, err
	}
	return []*meta.RESTMapping{mapping}, nil
}

// ResourceFor implements meta.RESTMapper.
func (m *StaticRESTMapper) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	// Try to find by GVR
	for _, mapping := range m.mappings {
		if mapping.Resource == input {
			return mapping.Resource, nil
		}
	}
	return schema.GroupVersionResource{}, &meta.NoResourceMatchError{PartialResource: input}
}

// ResourcesFor implements meta.RESTMapper.
func (m *StaticRESTMapper) ResourcesFor(input schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	gvr, err := m.ResourceFor(input)
	if err != nil {
		return nil, err
	}
	return []schema.GroupVersionResource{gvr}, nil
}

// KindFor implements meta.RESTMapper.
func (m *StaticRESTMapper) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	for gvk, mapping := range m.mappings {
		if mapping.Resource == resource {
			return gvk, nil
		}
	}
	return schema.GroupVersionKind{}, &meta.NoResourceMatchError{PartialResource: resource}
}

// KindsFor implements meta.RESTMapper.
func (m *StaticRESTMapper) KindsFor(resource schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	gvk, err := m.KindFor(resource)
	if err != nil {
		return nil, err
	}
	return []schema.GroupVersionKind{gvk}, nil
}

// ResourceSingularizer implements meta.RESTMapper.
func (m *StaticRESTMapper) ResourceSingularizer(resource string) (singular string, err error) {
	// Simple heuristic: remove trailing 's'
	if strings.HasSuffix(resource, "s") {
		return resource[:len(resource)-1], nil
	}
	return resource, nil
}

// Add method to help with testing and extension
func (m *StaticRESTMapper) Add(gvk schema.GroupVersionKind, gvr schema.GroupVersionResource, scope meta.RESTScope) {
	m.mappings[gvk] = meta.RESTMapping{
		Resource:         gvr,
		GroupVersionKind: gvk,
		Scope:            scope,
	}
}
