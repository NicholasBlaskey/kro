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

package lsp

import (
	"github.com/spf13/cobra"
)

var (
	offline  bool
	logLevel string
)

func AddLSPCommands(root *cobra.Command) {
	lspCmd := &cobra.Command{
		Use:   "lsp",
		Short: "Language Server Protocol (LSP) for Kro ResourceGraphDefinitions",
		Long: `Language Server Protocol support for Kro ResourceGraphDefinitions.

The LSP server provides IDE features like:
- Real-time validation with accurate error positioning
- Auto-completion for resource IDs, fields, and CEL functions
- Hover documentation for resources and dependencies
- Go-to-definition for resource references
- Document outline view

Use 'kro lsp server' to start the LSP server for your editor.`,
	}

	lspCmd.PersistentFlags().BoolVar(&offline, "offline", false, "Run in offline mode (no cluster connection required)")
	lspCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	lspCmd.AddCommand(newServerCommand())

	root.AddCommand(lspCmd)
}
