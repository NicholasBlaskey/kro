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

package server

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime/debug"

	"github.com/go-logr/logr"
	"github.com/kro-run/kro/tools/lsp/server/services"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"github.com/tliron/glsp/server"
	"k8s.io/client-go/rest"
)

// normalizeURI resolves symlinks in the file path and returns a canonical file:// URI
func normalizeURI(fileURI string) string {
	// Parse the URI
	u, err := url.Parse(fileURI)
	if err != nil {
		return fileURI // Return original on parse error
	}

	// Only process file:// URIs
	if u.Scheme != "file" {
		return fileURI
	}

	// Get the path from the URI
	path := u.Path

	// Resolve symlinks to get canonical path
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		// If we can't resolve, return original URI
		return fileURI
	}

	// If the path changed, rebuild the URI with the real path
	if realPath != path {
		u.Path = realPath
		return u.String()
	}

	return fileURI
}

func getVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	// Prefer the module version if it's set (and not a local dev build)
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	// Fall back to git info if available
	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}

	if revision != "" {
		version := fmt.Sprintf("git-%s", revision[:min(7, len(revision))])
		if modified == "true" {
			version += "-dirty"
		}
		return version
	}

	// Default for local or untagged builds
	return "dev"
}

// KroServer represents the main KRO Language Server instance that implements
// the Language Server Protocol (LSP) for KRO ResourceGraphDefinition files.
// It handles document lifecycle, validation, and communication with LSP clients.
type KroServer struct {
	logger                 logr.Logger
	documentManager        *DocumentManager
	server                 *server.Server
	currentContext         *glsp.Context
	notifyFunc             glsp.NotifyFunc
	definitionLinkSupport  bool
}

// NewKroServer creates a new KRO Language Server instance with the provided configuration.
// Parameters:
//   - logger: Structured logger for server operations
//   - clientConfig: Kubernetes client configuration (nil for offline mode)
//   - lspServer: GLSP server instance (can be nil, will be set later)
func NewKroServer(logger logr.Logger, clientConfig *rest.Config, lspServer *server.Server) *KroServer {
	return &KroServer{
		logger:          logger,
		documentManager: NewDocumentManager(logger, clientConfig),
		server:          lspServer,
	}
}

// SetServer sets the GLSP server instance after creation
func (s *KroServer) SetServer(lspServer *server.Server) {
	s.server = lspServer
}

// PublishDiagnostics sends validation diagnostics (errors, warnings) to the LSP client.
// This is used to display real-time validation results in the editor.
func (s *KroServer) PublishDiagnostics(uri string, version int32, diagnostics []protocol.Diagnostic) {
	if diagnostics == nil {
		diagnostics = []protocol.Diagnostic{}
	}

	if s.notifyFunc == nil {
		s.logger.Error(nil, "Cannot publish diagnostics: no notify function available", "uri", uri)
		return
	}

	// Build diagnostics as raw map to bypass any struct serialization issues
	rawDiags := make([]map[string]interface{}, 0, len(diagnostics))
	for _, d := range diagnostics {
		diag := map[string]interface{}{
			"range": map[string]interface{}{
				"start": map[string]interface{}{
					"line":      d.Range.Start.Line,
					"character": d.Range.Start.Character,
				},
				"end": map[string]interface{}{
					"line":      d.Range.End.Line,
					"character": d.Range.End.Character,
				},
			},
			"message": d.Message,
		}
		if d.Severity != nil {
			diag["severity"] = int(*d.Severity)
		}
		if d.Source != nil {
			diag["source"] = *d.Source
		}
		rawDiags = append(rawDiags, diag)
	}

	params := map[string]interface{}{
		"uri":         uri,
		"diagnostics": rawDiags,
	}

	s.logger.Info("Publishing diagnostics via raw map", "uri", uri, "count", len(diagnostics))

	jsonBytes, _ := json.Marshal(params)
	s.logger.Info("Raw JSON being sent", "json", string(jsonBytes))

	s.notifyFunc("textDocument/publishDiagnostics", params)

	s.logger.Info("Notification sent successfully")
}

// Initialize handles the LSP initialization request from the client.
// This is the first method called when the LSP client connects.
func (s *KroServer) Initialize(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
	s.logger.Info("Initializing Kro Language Server")
	s.currentContext = context
	s.notifyFunc = context.Notify

	// Check if client supports definition links
	if params.Capabilities.TextDocument != nil &&
	   params.Capabilities.TextDocument.Definition != nil &&
	   params.Capabilities.TextDocument.Definition.LinkSupport != nil {
		s.definitionLinkSupport = *params.Capabilities.TextDocument.Definition.LinkSupport
	}

	// Define what capabilities this LSP server supports
	capabilities := s.createServerCapabilities()

	// Return initialization result with server capabilities and info
	return protocol.InitializeResult{
		Capabilities: capabilities, // What features this server supports
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    "Kro Language Server",
			Version: new(getVersion()),
		},
	}, nil
}

// Initialized handles the initialized notification from the client.
// Called after successful initialization to indicate the server is ready.
func (s *KroServer) Initialized(context *glsp.Context, params *protocol.InitializedParams) error {
	s.logger.Info("Server initialized successfully")
	s.currentContext = context
	// Set up the document manager to send notifications through this server
	s.documentManager.SetNotificationSender(s)
	return nil
}

// Shutdown handles the shutdown request from the client.
// Performs cleanup before the server terminates.
func (s *KroServer) Shutdown(context *glsp.Context) error {
	s.logger.Info("Shutting down server")
	return nil
}

// SetTrace handles trace setting changes from the client.
// Used for debugging and logging level control.
func (s *KroServer) SetTrace(context *glsp.Context, params *protocol.SetTraceParams) error {
	return nil
}

// Document lifecycle methods - handle document state changes in the editor

// DidOpen handles when a document is opened in the editor.
// Triggers initial validation of the KRO ResourceGraphDefinition.
func (s *KroServer) DidOpen(context *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	uri := params.TextDocument.URI         // Document identifier
	version := params.TextDocument.Version // Document version for change tracking
	content := params.TextDocument.Text    // Full document content
	s.currentContext = context

	// Normalize URI to resolve symlinks
	normalizedURI := normalizeURI(uri)
	if normalizedURI != uri {
		s.logger.Info("DidOpen URI normalized", "original", uri, "normalized", normalizedURI)
		uri = normalizedURI
	}

	// Log exact URI received from client
	s.logger.Info("DidOpen received", "uri", uri, "version", version, "languageId", params.TextDocument.LanguageID)

	// Register the document and trigger validation
	s.documentManager.OpenDocument(uri, version, content)
	return nil
}

// DidChange handles when document content changes in the editor.
// Triggers re-validation with the updated content for real-time feedback.
func (s *KroServer) DidChange(context *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	uri := params.TextDocument.URI         // Document identifier
	version := params.TextDocument.Version // New document version
	s.currentContext = context

	// No changes to process
	if len(params.ContentChanges) == 0 {
		return nil
	}

	var finalContent string
	var hasContent bool

	// Process all content changes - LSP can send multiple change events
	for _, change := range params.ContentChanges {
		var newContent string

		// Handle different types of change events (incremental vs full document)
		if changeEvent, ok := change.(protocol.TextDocumentContentChangeEvent); ok {
			newContent = changeEvent.Text
		} else if changeEvent, ok := change.(protocol.TextDocumentContentChangeEventWhole); ok {
			newContent = changeEvent.Text
		} else if changeMap, ok := change.(map[string]interface{}); ok {
			// Fallback for generic change format
			if text, textOk := changeMap["text"].(string); textOk {
				newContent = text
			}
		}

		// Keep the last valid content change
		if newContent != "" {
			finalContent = newContent
			hasContent = true
		}
	}

	// Update document with new content and trigger validation
	if hasContent {
		s.documentManager.UpdateDocument(uri, version, finalContent)
	}

	return nil
}

// DidClose handles when a document is closed in the editor.
// Cleans up document state and stops validation.
func (s *KroServer) DidClose(context *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI
	s.currentContext = context
	// Remove document from management and clear diagnostics
	s.documentManager.CloseDocument(uri)
	return nil
}

// DidSave handles when a document is saved in the editor.
// Triggers re-validation to ensure saved content is valid.
func (s *KroServer) DidSave(context *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
	uri := params.TextDocument.URI
	s.currentContext = context

	// Re-validate the document with current content
	if doc, exists := s.documentManager.GetDocument(uri); exists {
		s.documentManager.UpdateDocument(uri, doc.Version, doc.Content)
	}

	return nil
}

// createServerCapabilities defines what LSP features this server supports.
// Currently supports full document synchronization and save notifications.
func (s *KroServer) createServerCapabilities() protocol.ServerCapabilities {
	// Use full document sync (client sends entire document on changes)
	capabilities := protocol.ServerCapabilities{
		TextDocumentSync: protocol.TextDocumentSyncOptions{
			OpenClose: new(true),                              // Handle document open/close events
			Change:    new(protocol.TextDocumentSyncKindFull), // Handle document change events (full sync)
			Save: &protocol.SaveOptions{
				IncludeText: new(true), // Include document text in save events
			},
		},
		HoverProvider:      boolPtr(true),
		DefinitionProvider: boolPtr(true),
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{".", "$", "{"},
		},
		// Future capabilities can be added here:
		// - CodeActionProvider (quick fixes)
	}

	return capabilities
}

// DidChangeWatchedFiles handles file system change notifications.
// Currently not implemented but required by the LSP protocol.
func (s *KroServer) DidChangeWatchedFiles(_ *glsp.Context, _ *protocol.DidChangeWatchedFilesParams) error {
	return nil
}

// CreateHandler creates the LSP protocol handler with all supported methods.
// This maps LSP protocol methods to server implementation functions.
func (s *KroServer) Completion(context *glsp.Context, params *protocol.CompletionParams) (any, error) {
	s.currentContext = context
	uri := params.TextDocument.URI
	position := params.Position

	// Get the document
	doc, exists := s.documentManager.GetDocument(uri)
	if !exists {
		return nil, nil
	}

	// Get symbol table and position map from document
	if doc.SymbolTable == nil {
		// No symbol table yet - validation hasn't completed or failed
		s.logger.Info("Completion: SymbolTable is nil", "uri", uri)
		return protocol.CompletionList{
			IsIncomplete: false,
			Items:        []protocol.CompletionItem{},
		}, nil
	}

	s.logger.Info("Completion: SymbolTable exists",
		"resourceCount", len(doc.SymbolTable.Resources),
		"hasSchema", doc.SymbolTable.Schema != nil)

	// Create completion provider
	provider := services.NewCompletionProvider(doc.SymbolTable, doc.PositionMap)

	// Get completions
	items := provider.ProvideCompletions(doc.Content, position)

	// Debug logging
	triggerKind := 0
	triggerChar := ""
	if params.Context != nil {
		triggerKind = int(params.Context.TriggerKind)
		if params.Context.TriggerCharacter != nil {
			triggerChar = *params.Context.TriggerCharacter
		}
	}

	// Log request details
	s.logger.Info("Completion request",
		"uri", uri,
		"line", position.Line,
		"char", position.Character,
		"triggerKind", triggerKind,
		"triggerChar", triggerChar,
		"itemCount", len(items))

	// Log the full JSON response we're sending
	if len(items) > 0 {
		jsonBytes, _ := json.MarshalIndent(items, "", "  ")
		s.logger.Info("Completion response JSON", "json", string(jsonBytes))
	}

	// Return a CompletionList with isIncomplete=true to keep popup open
	// This tells the client to re-request completions as the user types
	return protocol.CompletionList{
		IsIncomplete: true,
		Items:        items,
	}, nil
}

// Hover handles the LSP textDocument/hover request.
// Provides hover information for resource IDs, schema, and special identifiers.
func (s *KroServer) Hover(context *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	s.currentContext = context
	uri := params.TextDocument.URI
	position := params.Position

	// Get the document
	doc, exists := s.documentManager.GetDocument(uri)
	if !exists {
		s.logger.V(1).Info("Hover: doc not found")
		return nil, nil
	}

	// Get symbol table and position map from document
	if doc.SymbolTable == nil {
		s.logger.V(1).Info("Hover: symbol table is nil")
		return nil, nil
	}

	s.logger.V(1).Info("Hover: have symbol table", "resources", len(doc.SymbolTable.Resources))

	// Create hover provider
	provider := services.NewHoverProvider(doc.SymbolTable, doc.PositionMap)

	// Get hover content
	hover := provider.ProvideHover(doc.Content, position)

	s.logger.V(1).Info("Hover: result", "hasHover", hover != nil)

	return hover, nil
}

// Definition handles the LSP textDocument/definition request.
// Provides go-to-definition for resource IDs, schema, and iterators.
func (s *KroServer) Definition(context *glsp.Context, params *protocol.DefinitionParams) (any, error) {
	s.currentContext = context
	uri := params.TextDocument.URI
	position := params.Position

	doc, exists := s.documentManager.GetDocument(uri)
	if !exists {
		return nil, nil
	}

	if doc.SymbolTable == nil {
		return nil, nil
	}

	provider := services.NewDefinitionProvider(doc.SymbolTable, doc.PositionMap, uri)
	links := provider.ProvideDefinition(doc.Content, position)

	s.logger.Info("Definition request",
		"line", position.Line,
		"char", position.Character,
		"linkSupport", s.definitionLinkSupport,
		"resultCount", len(links))

	if len(links) > 0 {
		s.logger.Info("Definition result",
			"originRange", fmt.Sprintf("%d:%d-%d:%d",
				links[0].OriginSelectionRange.Start.Line, links[0].OriginSelectionRange.Start.Character,
				links[0].OriginSelectionRange.End.Line, links[0].OriginSelectionRange.End.Character),
			"targetRange", fmt.Sprintf("%d:%d-%d:%d",
				links[0].TargetRange.Start.Line, links[0].TargetRange.Start.Character,
				links[0].TargetRange.End.Line, links[0].TargetRange.End.Character))

		jsonBytes, _ := json.MarshalIndent(links, "", "  ")
		s.logger.Info("Definition response JSON", "json", string(jsonBytes))
	}

	if s.definitionLinkSupport {
		return links, nil
	}

	if len(links) == 0 {
		return nil, nil
	}
	return []protocol.Location{
		{
			URI:   links[0].TargetURI,
			Range: links[0].TargetRange,
		},
	}, nil
}

func (s *KroServer) CreateHandler() *protocol.Handler {
	handler := &protocol.Handler{
		// Lifecycle methods - server startup and shutdown
		Initialize:  s.Initialize,
		Initialized: s.Initialized,
		Shutdown:    s.Shutdown,

		// Document synchronization methods - handle document state changes
		TextDocumentDidOpen:   s.DidOpen,
		TextDocumentDidChange: s.DidChange,
		TextDocumentDidClose:  s.DidClose,
		TextDocumentDidSave:   s.DidSave,

		// Workspace methods - handle workspace-level changes
		WorkspaceDidChangeWatchedFiles: s.DidChangeWatchedFiles,

		// Optional notifications
		SetTrace: s.SetTrace,

		// Language feature methods
		TextDocumentCompletion: s.Completion,
		TextDocumentHover:      s.Hover,
		TextDocumentDefinition: s.Definition,
	}

	return handler
}

// Utility functions for creating pointers to primitive types
// Required because LSP protocol uses pointers for optional fields

// stringPtr creates a pointer to a string value
func stringPtr(s string) *string {
	return &s
}

// boolPtr creates a pointer to a boolean value
func boolPtr(b bool) *bool {
	return &b
}
