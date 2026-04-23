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

package validate

import (
	"fmt"
	"os"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
	kroclient "github.com/kubernetes-sigs/kro/pkg/client"
	"github.com/kubernetes-sigs/kro/pkg/graph"
	schemaresolver "github.com/kubernetes-sigs/kro/pkg/graph/schema/resolver"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the ResourceGraphDefinition",
	Long: `Validate the ResourceGraphDefinition. This command checks ` +
		`if the ResourceGraphDefinition is valid and can be used to create a ResourceGraph.`,
}

var (
	resourceGroupDefinitionFile string
	fromCluster                 bool
	crdsDirectory               string
	kubernetesVersion           string
)

func init() {
	validateRGDCmd.PersistentFlags().StringVarP(&resourceGroupDefinitionFile, "file", "f", "",
		"Path to the ResourceGroupDefinition file")
	validateRGDCmd.PersistentFlags().BoolVar(&fromCluster, "from-cluster", false,
		"Query schemas from connected cluster")
	validateRGDCmd.PersistentFlags().StringVar(&crdsDirectory, "crds", "",
		"Load CRD schemas from local directory")
	validateRGDCmd.PersistentFlags().StringVar(&kubernetesVersion, "kubernetes-version", "",
		"Use built-in Kubernetes version schemas (e.g., '1.30')")
}

var validateRGDCmd = &cobra.Command{
	Use:   "rgd [FILE]",
	Short: "Validate a ResourceGraphDefinition file",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return validateFlags()
	},
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

		if err := validateRGD(&rgd); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}

		fmt.Println("Validation successful! The ResourceGraphDefinition is valid.")
		return nil
	},
}

func validateFlags() error {
	// --from-cluster is mutually exclusive with other modes
	if fromCluster && (crdsDirectory != "" || kubernetesVersion != "") {
		return fmt.Errorf("--from-cluster cannot be used with --crds or --kubernetes-version")
	}

	return nil
}

func validateRGD(rgd *v1alpha1.ResourceGraphDefinition) error {
	var builder *graph.Builder
	var err error

	if fromCluster {
		// Cluster mode: use existing behavior
		set, err := kroclient.NewSet(kroclient.Config{})
		if err != nil {
			return fmt.Errorf("failed to create client set: %w", err)
		}

		builder, err = graph.NewBuilder(set.RESTConfig(), set.HTTPClient())
		if err != nil {
			return fmt.Errorf("failed to create graph builder: %w", err)
		}
	} else {
		// Offline modes: use factory
		mode := determineMode()
		cfg := schemaresolver.ComponentConfig{
			Mode:              mode,
			CRDDirectory:      crdsDirectory,
			KubernetesVersion: kubernetesVersion,
		}

		components, err := schemaresolver.NewComponents(cfg)
		if err != nil {
			return fmt.Errorf("failed to create resolver components: %w", err)
		}

		builder = graph.NewBuilderWithComponents(
			components.SchemaResolver,
			components.RESTMapper,
		)
	}

	_, err = builder.NewResourceGraphDefinition(rgd, graph.RGDConfig{
		MaxCollectionSize:          100,
		MaxCollectionDimensionSize: 100,
	})
	if err != nil {
		return fmt.Errorf("failed to validate ResourceGraphDefinition: %w", err)
	}

	return nil
}

func determineMode() string {
	if crdsDirectory != "" && kubernetesVersion != "" {
		return "combined"
	}
	if crdsDirectory != "" {
		return "crds"
	}
	if kubernetesVersion != "" {
		return "k8s-version"
	}
	return "offline" // default: empty offline mode
}

func AddValidateCommands(rootCmd *cobra.Command) {
	validateCmd.AddCommand(validateRGDCmd)
	rootCmd.AddCommand(validateCmd)
}
