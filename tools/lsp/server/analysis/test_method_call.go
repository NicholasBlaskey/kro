package analysis

import (
	"fmt"
	"testing"
)

func TestMethodCallVsReference(t *testing.T) {
	symbolTable := NewSymbolTable()
	symbolTable.Schema = &SchemaSymbol{
		SpecFields: map[string]*FieldSymbol{
			"name": {Name: "name", Type: "string"},
		},
	}
	
	tests := []struct {
		expr     string
		expected CELType
	}{
		// Method references should return unknown
		{"schema.spec.name.replace", CELTypeUnknown},
		
		// Method CALLS should return their return type
		{"schema.spec.name.replace('x', 'y')", CELTypeString},
		{"schema.spec.name.split('-')", CELTypeList},
	}
	
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			result := InferCELExpressionType(tt.expr, symbolTable)
			fmt.Printf("Expression: %s\n  Got: %s, Want: %s\n", tt.expr, result, tt.expected)
			if result != tt.expected {
				t.Errorf("InferCELExpressionType(%q) = %v, want %v", tt.expr, result, tt.expected)
			}
		})
	}
}
