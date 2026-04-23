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
	"os"
	"path/filepath"
	"strings"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/yaml"

	graphschema "github.com/kubernetes-sigs/kro/pkg/graph/schema"
)

// CRDDirectoryResolver loads CRD schemas from a local directory.
type CRDDirectoryResolver struct {
	schemas map[schema.GroupVersionKind]*spec.Schema
}

// NewCRDDirectoryResolver creates a resolver that loads CRD schemas from a directory.
// It walks the directory for .yaml, .yml, and .json files, parses them as CRDs,
// and extracts OpenAPI schemas for each version.
func NewCRDDirectoryResolver(directory string) (*CRDDirectoryResolver, []*extv1.CustomResourceDefinition, error) {
	if directory == "" {
		return nil, nil, fmt.Errorf("CRD directory path is required")
	}

	info, err := os.Stat(directory)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to access CRD directory %s: %w", directory, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("path %s is not a directory", directory)
	}

	resolver := &CRDDirectoryResolver{
		schemas: make(map[schema.GroupVersionKind]*spec.Schema),
	}

	var crds []*extv1.CustomResourceDefinition

	err = filepath.WalkDir(directory, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Only process YAML and JSON files
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", path, err)
		}

		crd := &extv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(data, crd); err != nil {
			// Skip files that aren't valid CRDs
			return nil
		}

		// Validate it's actually a CRD
		if crd.Kind != "CustomResourceDefinition" {
			return nil
		}

		if err := resolver.addCRD(crd, path); err != nil {
			return fmt.Errorf("failed to process CRD from %s: %w", path, err)
		}

		crds = append(crds, crd)
		return nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to walk CRD directory: %w", err)
	}

	return resolver, crds, nil
}

// addCRD extracts schemas from a CRD and adds them to the resolver.
func (r *CRDDirectoryResolver) addCRD(crd *extv1.CustomResourceDefinition, sourcePath string) error {
	// Check for webhook conversion
	if crd.Spec.Conversion != nil && crd.Spec.Conversion.Strategy == extv1.WebhookConverter {
		return fmt.Errorf("CRD %s uses webhook conversion which requires cluster access. Use --from-cluster instead", crd.Name)
	}

	group := crd.Spec.Group
	kind := crd.Spec.Names.Kind

	// Extract schema for each version
	for _, version := range crd.Spec.Versions {
		if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			continue
		}

		gvk := schema.GroupVersionKind{
			Group:   group,
			Version: version.Name,
			Kind:    kind,
		}

		// Convert JSONSchemaProps to spec.Schema
		openAPISchema, err := graphschema.ConvertJSONSchemaPropsToSpecSchema(version.Schema.OpenAPIV3Schema)
		if err != nil {
			return fmt.Errorf("failed to convert schema for %s: %w", gvk, err)
		}

		// Check for duplicate GVKs (last-wins strategy, log warning)
		if existing, exists := r.schemas[gvk]; exists {
			// Just overwrite - matches kubectl behavior
			_ = existing // avoid unused variable
		}

		r.schemas[gvk] = openAPISchema
	}

	return nil
}

// ResolveSchema implements resolver.SchemaResolver.
func (r *CRDDirectoryResolver) ResolveSchema(gvk schema.GroupVersionKind) (*spec.Schema, error) {
	schema, ok := r.schemas[gvk]
	if !ok {
		// Build list of available versions for this group/kind
		var availableVersions []string
		for k := range r.schemas {
			if k.Group == gvk.Group && k.Kind == gvk.Kind {
				availableVersions = append(availableVersions, k.Version)
			}
		}

		if len(availableVersions) > 0 {
			return nil, fmt.Errorf("schema not found for %s (available versions: %v)", gvk, availableVersions)
		}

		return nil, fmt.Errorf("schema not found for %s", gvk)
	}
	return schema, nil
}
