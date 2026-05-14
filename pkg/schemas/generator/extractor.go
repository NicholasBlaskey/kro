package main

import (
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/apiextensions-apiserver/pkg/generated/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <version>\n", os.Args[0])
		os.Exit(1)
	}

	version := os.Args[1]
	defs := openapi.GetOpenAPIDefinitions(func(path string) spec.Ref {
		return spec.MustCreateRef(path)
	})

	data, err := json.Marshal(defs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal: %v\n", err)
		os.Exit(1)
	}

	outFile := fmt.Sprintf("../schemas/schemas_%s.json", version)
	if err := os.WriteFile(outFile, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("  ✓ Generated %s (%d definitions)\n", outFile, len(defs))
}
