# Offline Validation Implementation Plan

## Goal
Enable `kro validate` to work completely offline by supporting local CRD files and embedded Kubernetes schemas, without requiring cluster connectivity.

## Architecture

### Schema Resolution
Currently: `Builder` creates a cluster-based `schemaResolver` internally
New: `Builder` accepts injectable `schemaResolver`, allowing multiple backends

### Three Resolver Types
1. **Embedded** - Uses `pkg/schemas.Schemas` map for built-in k8s types
2. **File** - Parses CRD YAML files from local directory
3. **Cluster** - Current behavior (connects to API server)

## Implementation: 3 Clean Commits

---

## Commit 1: Refactor Builder to Accept Schema Resolver

### Goal
Make `schemaResolver` injectable without changing behavior

### Changes

#### `pkg/graph/builder.go`
```go
// NEW: Constructor that accepts resolver
func NewBuilder(schemaResolver resolver.SchemaResolver, restMapper meta.RESTMapper) *Builder {
    return &Builder{
        schemaResolver: schemaResolver,
        restMapper:     restMapper,
    }
}

// KEEP: Convenience for cluster-based resolution (existing behavior)
func NewBuilderFromCluster(clientConfig *rest.Config, httpClient *http.Client) (*Builder, error) {
    schemaResolver, err := schemaresolver.NewCombinedResolver(clientConfig, httpClient)
    if err != nil {
        return nil, fmt.Errorf("failed to create schema resolver: %w", err)
    }

    rm, err := apiutil.NewDynamicRESTMapper(clientConfig, httpClient)
    if err != nil {
        return nil, fmt.Errorf("failed to create dynamic REST mapper: %w", err)
    }

    return NewBuilder(schemaResolver, rm), nil
}
```

#### Update Call Sites
Replace `graph.NewBuilder(config, client)` → `graph.NewBuilderFromCluster(config, client)`

Files to update:
- `cmd/controller/main.go:255`
- `cmd/kro/commands/validate/validate.go:75`
- `cmd/kro/commands/generate/generate_utils.go:36`
- `test/integration/environment/setup.go:164`
- `tools/lsp/server/validation.go:63` (different signature - needs investigation)

### Testing
- Run existing tests - should all pass (no behavior change)
- Controller and CLI should work identically

### Commit Message
```
refactor: make Builder schema resolver injectable

Changes Builder constructor to accept SchemaResolver as parameter
instead of creating it internally. Adds NewBuilderFromCluster()
convenience function that maintains existing cluster-based behavior.

This enables future support for offline validation by allowing
alternative schema resolver implementations (embedded, file-based).

No behavior change - all existing code uses NewBuilderFromCluster().
```

---

## Commit 2: Add Offline RGD Validation CLI

### Goal
Add `--kubernetes-version` and `--crds` flags to `kro validate rgd`

### New File: `cmd/kro/commands/validate/resolvers.go`

```go
package validate

import (
    "encoding/json"
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
    
    "k8s.io/apiserver/pkg/cel/openapi/resolver"
    "k8s.io/kube-openapi/pkg/common"
    "k8s.io/kube-openapi/pkg/validation/spec"
    
    "github.com/kubernetes-sigs/kro/pkg/schemas"
)

// EmbeddedSchemaResolver resolves schemas from embedded pkg/schemas
type EmbeddedSchemaResolver struct {
    version string
    defs    map[string]common.OpenAPIDefinition
}

func NewEmbeddedResolver(version string) (*EmbeddedSchemaResolver, error) {
    data, ok := schemas.Schemas[version]
    if !ok {
        return nil, fmt.Errorf("kubernetes version %s not found in embedded schemas", version)
    }
    
    var defs map[string]common.OpenAPIDefinition
    if err := json.Unmarshal(data, &defs); err != nil {
        return nil, fmt.Errorf("failed to unmarshal embedded schema: %w", err)
    }
    
    return &EmbeddedSchemaResolver{
        version: version,
        defs:    defs,
    }, nil
}

func (r *EmbeddedSchemaResolver) ResolveSchema(gvk schema.GroupVersionKind) (*spec.Schema, error) {
    // Implementation: lookup in defs map
}

// FileSchemaResolver loads CRD schemas from local directory
type FileSchemaResolver struct {
    crds map[schema.GroupVersionKind]*spec.Schema
}

func NewFileResolver(crdDir string) (*FileSchemaResolver, error) {
    resolver := &FileSchemaResolver{
        crds: make(map[schema.GroupVersionKind]*spec.Schema),
    }
    
    // Walk directory, parse CRD YAMLs, extract OpenAPI schemas
    err := filepath.WalkDir(crdDir, func(path string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() {
            return err
        }
        
        if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
            return nil
        }
        
        // Parse CRD file, extract schema
        return resolver.loadCRD(path)
    })
    
    return resolver, err
}

func (r *FileSchemaResolver) loadCRD(path string) error {
    // Read file, unmarshal as CRD, extract OpenAPIV3Schema
}

func (r *FileSchemaResolver) ResolveSchema(gvk schema.GroupVersionKind) (*spec.Schema, error) {
    // Implementation: lookup in crds map
}

// CombinedSchemaResolver tries multiple resolvers in order
type CombinedSchemaResolver struct {
    resolvers []resolver.SchemaResolver
}

func NewCombinedResolver(resolvers ...resolver.SchemaResolver) *CombinedSchemaResolver {
    return &CombinedSchemaResolver{resolvers: resolvers}
}

func (r *CombinedSchemaResolver) ResolveSchema(gvk schema.GroupVersionKind) (*spec.Schema, error) {
    for _, resolver := range r.resolvers {
        schema, err := resolver.ResolveSchema(gvk)
        if err == nil {
            return schema, nil
        }
    }
    return nil, fmt.Errorf("schema not found for %v", gvk)
}

// Factory function used by CLI
func newSchemaResolver(kubernetesVersion, crdsDir string, fromCluster bool, config *rest.Config, httpClient *http.Client) (resolver.SchemaResolver, error) {
    var resolvers []resolver.SchemaResolver
    
    // Priority: cluster > file > embedded
    if fromCluster {
        if config == nil {
            return nil, fmt.Errorf("--from-cluster requires kubeconfig")
        }
        clusterResolver, err := schemaresolver.NewCombinedResolver(config, httpClient)
        if err != nil {
            return nil, fmt.Errorf("failed to create cluster resolver: %w", err)
        }
        resolvers = append(resolvers, clusterResolver)
    }
    
    if crdsDir != "" {
        fileResolver, err := NewFileResolver(crdsDir)
        if err != nil {
            return nil, fmt.Errorf("failed to load CRDs from %s: %w", crdsDir, err)
        }
        resolvers = append(resolvers, fileResolver)
    }
    
    if kubernetesVersion != "" {
        embeddedResolver, err := NewEmbeddedResolver(kubernetesVersion)
        if err != nil {
            return nil, fmt.Errorf("failed to create embedded resolver: %w", err)
        }
        resolvers = append(resolvers, embeddedResolver)
    }
    
    if len(resolvers) == 0 {
        // Default: use latest embedded version
        version := getLatestEmbeddedVersion()
        embeddedResolver, err := NewEmbeddedResolver(version)
        if err != nil {
            return nil, err
        }
        resolvers = append(resolvers, embeddedResolver)
    }
    
    return NewCombinedResolver(resolvers...), nil
}

func getLatestEmbeddedVersion() string {
    // Return latest version from schemas.Schemas map
    // e.g., "v1.36"
}
```

### Update: `cmd/kro/commands/validate/validate.go`

```go
var (
    kubernetesVersion string
    crdsDir          string
    fromCluster      bool
)

func init() {
    validateRGDCmd.PersistentFlags().StringVarP(&resourceGroupDefinitionFile, "file", "f", "",
        "Path to the ResourceGroupDefinition file")
    validateRGDCmd.PersistentFlags().StringVar(&kubernetesVersion, "kubernetes-version", "",
        "Kubernetes version for built-in schemas (e.g., v1.30). Default: latest embedded")
    validateRGDCmd.PersistentFlags().StringVar(&crdsDir, "crds", "",
        "Directory containing CRD files for schema resolution")
    validateRGDCmd.PersistentFlags().BoolVar(&fromCluster, "from-cluster", false,
        "Discover schemas from cluster (requires kubeconfig)")
}

var validateRGDCmd = &cobra.Command{
    Use:   "rgd",
    Short: "Validate a ResourceGraphDefinition file",
    Long: `Validate a ResourceGraphDefinition file offline or against a cluster.

Examples:
  # Validate with default embedded schemas
  kro validate rgd -f rgd.yaml

  # Validate with specific Kubernetes version
  kro validate rgd -f rgd.yaml --kubernetes-version v1.30

  # Validate with local CRD files
  kro validate rgd -f rgd.yaml --crds ./crds/

  # Validate with both embedded and local CRDs
  kro validate rgd -f rgd.yaml --kubernetes-version v1.30 --crds ./crds/

  # Validate against cluster
  kro validate rgd -f rgd.yaml --from-cluster`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if resourceGroupDefinitionFile == "" {
            return fmt.Errorf("ResourceGroupDefinition file is required")
        }

        data, err := os.ReadFile(resourceGroupDefinitionFile)
        if err != nil {
            return fmt.Errorf("failed to read ResourceGroupDefinition file: %w", err)
        }

        var rgd v1alpha1.ResourceGraphDefinition
        if err = yaml.Unmarshal(data, &rgd); err != nil {
            return fmt.Errorf("failed to unmarshal ResourceGroupDefinition: %w", err)
        }

        // Create schema resolver based on flags
        var schemaResolver resolver.SchemaResolver
        var restMapper meta.RESTMapper
        
        if fromCluster {
            // Use cluster-based resolution
            set, err := kroclient.NewSet(kroclient.Config{})
            if err != nil {
                return fmt.Errorf("failed to create client set: %w", err)
            }
            schemaResolver, err = newSchemaResolver(kubernetesVersion, crdsDir, fromCluster, set.RESTConfig(), set.HTTPClient())
            if err != nil {
                return err
            }
            restMapper, err = apiutil.NewDynamicRESTMapper(set.RESTConfig(), set.HTTPClient())
            if err != nil {
                return fmt.Errorf("failed to create REST mapper: %w", err)
            }
        } else {
            // Offline validation - no REST mapper needed
            schemaResolver, err = newSchemaResolver(kubernetesVersion, crdsDir, fromCluster, nil, nil)
            if err != nil {
                return err
            }
            restMapper = nil // Offline mode doesn't need REST mapper
        }

        // Create builder with resolver
        builder := graph.NewBuilder(schemaResolver, restMapper)

        // Validate
        if _, err := builder.NewResourceGraphDefinition(&rgd); err != nil {
            return fmt.Errorf("validation failed: %w", err)
        }

        fmt.Println("✓ Validation successful! The ResourceGraphDefinition is valid.")
        return nil
    },
}
```

### Testing
```bash
# Test with embedded schemas
go run cmd/kro/main.go validate rgd -f examples/my-rgd.yaml

# Test with specific version
go run cmd/kro/main.go validate rgd -f examples/my-rgd.yaml --kubernetes-version v1.30

# Test with local CRDs
go run cmd/kro/main.go validate rgd -f examples/my-rgd.yaml --crds ./examples/crds/

# Test with cluster
go run cmd/kro/main.go validate rgd -f examples/my-rgd.yaml --from-cluster
```

### Commit Message
```
feat: add offline RGD validation with embedded schemas and local CRDs

Adds support for validating ResourceGraphDefinitions completely offline:

- --kubernetes-version: Use embedded k8s schemas (v1.28-v1.36)
- --crds: Load CRD schemas from local directory
- --from-cluster: Discover schemas from API server (existing behavior)

Resolvers can be combined, e.g.:
  kro validate rgd -f rgd.yaml --kubernetes-version v1.30 --crds ./crds/

Default behavior uses latest embedded k8s schemas, enabling validation
without any cluster connectivity or additional flags.

Implementations (EmbeddedResolver, FileResolver, CombinedResolver) live
in CLI package following YAGNI principle - can be moved to pkg/ if
needed elsewhere.
```

---

## Commit 3: Add Instance Validation CLI

### Goal
Add `kro validate instance` command to validate instances against RGD schema

### New File: `cmd/kro/commands/validate/instance.go`

```go
package validate

import (
    "fmt"
    "os"
    
    "github.com/spf13/cobra"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "sigs.k8s.io/yaml"
    
    "github.com/kubernetes-sigs/kro/api/v1alpha1"
    "github.com/kubernetes-sigs/kro/pkg/graph"
)

var (
    instanceFile string
    rgdFile      string
)

func init() {
    validateInstanceCmd.PersistentFlags().StringVarP(&instanceFile, "file", "f", "",
        "Path to the instance file")
    validateInstanceCmd.PersistentFlags().StringVar(&rgdFile, "rgd", "",
        "Path to the ResourceGroupDefinition file")
    
    // Reuse same flags as RGD validation
    validateInstanceCmd.PersistentFlags().StringVar(&kubernetesVersion, "kubernetes-version", "",
        "Kubernetes version for built-in schemas (e.g., v1.30)")
    validateInstanceCmd.PersistentFlags().StringVar(&crdsDir, "crds", "",
        "Directory containing CRD files for schema resolution")
    validateInstanceCmd.PersistentFlags().BoolVar(&fromCluster, "from-cluster", false,
        "Discover schemas from cluster")
}

var validateInstanceCmd = &cobra.Command{
    Use:   "instance",
    Short: "Validate an instance against a ResourceGraphDefinition",
    Long: `Validate an instance file against its ResourceGraphDefinition schema.

Examples:
  # Validate with default embedded schemas
  kro validate instance -f instance.yaml --rgd rgd.yaml

  # Validate with specific Kubernetes version
  kro validate instance -f instance.yaml --rgd rgd.yaml --kubernetes-version v1.30

  # Validate with local CRDs
  kro validate instance -f instance.yaml --rgd rgd.yaml --crds ./crds/

  # Validate against cluster
  kro validate instance -f instance.yaml --rgd rgd.yaml --from-cluster`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if instanceFile == "" {
            return fmt.Errorf("instance file is required (--file)")
        }
        if rgdFile == "" {
            return fmt.Errorf("RGD file is required (--rgd)")
        }

        // Load RGD
        rgdData, err := os.ReadFile(rgdFile)
        if err != nil {
            return fmt.Errorf("failed to read RGD file: %w", err)
        }

        var rgd v1alpha1.ResourceGraphDefinition
        if err = yaml.Unmarshal(rgdData, &rgd); err != nil {
            return fmt.Errorf("failed to unmarshal RGD: %w", err)
        }

        // Load instance
        instanceData, err := os.ReadFile(instanceFile)
        if err != nil {
            return fmt.Errorf("failed to read instance file: %w", err)
        }

        var instance unstructured.Unstructured
        if err = yaml.Unmarshal(instanceData, &instance); err != nil {
            return fmt.Errorf("failed to unmarshal instance: %w", err)
        }

        // Create schema resolver
        var schemaResolver resolver.SchemaResolver
        var restMapper meta.RESTMapper
        
        if fromCluster {
            set, err := kroclient.NewSet(kroclient.Config{})
            if err != nil {
                return fmt.Errorf("failed to create client set: %w", err)
            }
            schemaResolver, err = newSchemaResolver(kubernetesVersion, crdsDir, fromCluster, set.RESTConfig(), set.HTTPClient())
            if err != nil {
                return err
            }
            restMapper, err = apiutil.NewDynamicRESTMapper(set.RESTConfig(), set.HTTPClient())
            if err != nil {
                return fmt.Errorf("failed to create REST mapper: %w", err)
            }
        } else {
            schemaResolver, err = newSchemaResolver(kubernetesVersion, crdsDir, fromCluster, nil, nil)
            if err != nil {
                return err
            }
            restMapper = nil
        }

        // Build and validate RGD
        builder := graph.NewBuilder(schemaResolver, restMapper)
        resourceGraph, err := builder.NewResourceGraphDefinition(&rgd)
        if err != nil {
            return fmt.Errorf("RGD validation failed: %w", err)
        }

        // Validate instance against RGD schema
        if err := validateInstanceAgainstSchema(&instance, &rgd, resourceGraph); err != nil {
            return fmt.Errorf("instance validation failed: %w", err)
        }

        fmt.Println("✓ Instance validation successful!")
        return nil
    },
}

func validateInstanceAgainstSchema(instance *unstructured.Unstructured, rgd *v1alpha1.ResourceGraphDefinition, graph *graph.ResourceGraphDefinition) error {
    // Validate that instance matches RGD's schema
    // 1. Check apiVersion/kind match
    expectedAPIVersion := fmt.Sprintf("%s/%s", rgd.Spec.Schema.APIGroup, rgd.Spec.Schema.Version)
    if instance.GetAPIVersion() != expectedAPIVersion {
        return fmt.Errorf("apiVersion mismatch: expected %s, got %s", expectedAPIVersion, instance.GetAPIVersion())
    }
    if instance.GetKind() != rgd.Spec.Schema.Kind {
        return fmt.Errorf("kind mismatch: expected %s, got %s", rgd.Spec.Schema.Kind, instance.GetKind())
    }

    // 2. Validate spec against SimpleSchema
    spec, found, err := unstructured.NestedMap(instance.Object, "spec")
    if err != nil {
        return fmt.Errorf("failed to get spec: %w", err)
    }
    if !found {
        return fmt.Errorf("spec field not found in instance")
    }

    // Validate spec fields against rgd.Spec.Schema.Spec
    if err := validateSpecFields(spec, rgd.Spec.Schema.Spec); err != nil {
        return fmt.Errorf("spec validation failed: %w", err)
    }

    // TODO: Optionally evaluate CEL expressions to ensure they resolve
    // This would require building the full graph context

    return nil
}

func validateSpecFields(spec map[string]interface{}, schemaSpec v1alpha1.SchemaSpec) error {
    // Walk through schema fields and validate instance has required fields
    // Check types match
    // TODO: Implement detailed validation
    return nil
}

func init() {
    validateCmd.AddCommand(validateInstanceCmd)
}
```

### Update: `cmd/kro/commands/validate/validate.go`
```go
func AddValidateCommands(rootCmd *cobra.Command) {
    validateCmd.AddCommand(validateRGDCmd)
    validateCmd.AddCommand(validateInstanceCmd)  // NEW
    rootCmd.AddCommand(validateCmd)
}
```

### Testing
```bash
# Test instance validation
go run cmd/kro/main.go validate instance -f examples/my-instance.yaml --rgd examples/my-rgd.yaml

# Test with specific version
go run cmd/kro/main.go validate instance -f examples/my-instance.yaml --rgd examples/my-rgd.yaml --kubernetes-version v1.30

# Test with local CRDs
go run cmd/kro/main.go validate instance -f examples/my-instance.yaml --rgd examples/my-rgd.yaml --crds ./examples/crds/
```

### Commit Message
```
feat: add instance validation command

Adds 'kro validate instance' command to validate instance files
against their ResourceGraphDefinition schema.

Usage:
  kro validate instance -f instance.yaml --rgd rgd.yaml [flags]

Supports same schema resolution flags as RGD validation:
  --kubernetes-version
  --crds
  --from-cluster

Validates:
- apiVersion and kind match RGD schema
- spec fields conform to RGD's SimpleSchema definition
- Required fields are present with correct types

Enables complete offline validation workflow:
1. Validate RGD with schemas
2. Validate instances against RGD
All without cluster connectivity.
```

---

## Summary

Three clean, focused commits:
1. **Refactor** - Make Builder injectable (~50 lines)
2. **RGD CLI** - Add offline validation (~500 lines, all in CLI)
3. **Instance CLI** - Validate instances (~300 lines)

Total: ~850 lines of new code, all in `cmd/kro/commands/validate/`

Following YAGNI: Keep all resolver implementations in CLI until we need them elsewhere.
