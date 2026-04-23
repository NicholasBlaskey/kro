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

package resolver

import (
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/cel/openapi/resolver"
	"k8s.io/client-go/rest"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/kubernetes-sigs/kro/pkg/graph/restmapper"
)

// ComponentConfig holds configuration for creating schema resolver and REST mapper.
type ComponentConfig struct {
	Mode string // "cluster", "offline", "crds", "k8s-version", "combined"

	// Cluster mode
	RESTConfig *rest.Config
	HTTPClient *http.Client

	// Offline modes
	CRDDirectory      string
	KubernetesVersion string
}

// Components contains the schema resolver and REST mapper for validation.
type Components struct {
	SchemaResolver resolver.SchemaResolver
	RESTMapper     meta.RESTMapper
}

// NewComponents creates schema resolver and REST mapper based on the configuration mode.
func NewComponents(cfg ComponentConfig) (*Components, error) {
	switch cfg.Mode {
	case "cluster":
		return newClusterComponents(cfg)
	case "offline":
		return newOfflineComponents(cfg)
	case "crds":
		return newCRDComponents(cfg)
	case "k8s-version":
		return newKubernetesVersionComponents(cfg)
	case "combined":
		return newCombinedComponents(cfg)
	default:
		return nil, fmt.Errorf("unknown resolver mode: %s", cfg.Mode)
	}
}

func newClusterComponents(cfg ComponentConfig) (*Components, error) {
	schemaResolver, err := NewCombinedResolver(cfg.RESTConfig, cfg.HTTPClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster schema resolver: %w", err)
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(cfg.RESTConfig, cfg.HTTPClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic REST mapper: %w", err)
	}

	return &Components{
		SchemaResolver: schemaResolver,
		RESTMapper:     restMapper,
	}, nil
}

func newOfflineComponents(cfg ComponentConfig) (*Components, error) {
	// Empty resolver - no schemas loaded
	emptyResolver := &emptyResolver{}
	emptyMapper := restmapper.NewEmptyRESTMapper()

	return &Components{
		SchemaResolver: emptyResolver,
		RESTMapper:     emptyMapper,
	}, nil
}

func newCRDComponents(cfg ComponentConfig) (*Components, error) {
	crdResolver, crds, err := NewCRDDirectoryResolver(cfg.CRDDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to create CRD resolver: %w", err)
	}

	crdMapper := restmapper.NewStaticRESTMapperFromCRDs(crds)

	return &Components{
		SchemaResolver: crdResolver,
		RESTMapper:     crdMapper,
	}, nil
}

func newKubernetesVersionComponents(cfg ComponentConfig) (*Components, error) {
	return nil, fmt.Errorf("kubernetes version mode not yet implemented")
}

func newCombinedComponents(cfg ComponentConfig) (*Components, error) {
	return nil, fmt.Errorf("combined mode not yet implemented")
}

// emptyResolver is a resolver that returns errors for all schema lookups.
type emptyResolver struct{}

func (e *emptyResolver) ResolveSchema(gvk schema.GroupVersionKind) (*spec.Schema, error) {
	return nil, fmt.Errorf("no schema resolver configured - use --from-cluster or --crds to provide schemas")
}
