package codemode

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGenerateFromMCPTools(t *testing.T) {
	t.Parallel()

	mcpTools := []mcp.Tool{
		{
			Name:        "search-products",
			Description: "Search the product catalog",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum result count",
					},
				},
				"required": []string{"query"},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{
						"type":        "array",
						"description": "Matched items",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id": map[string]any{
									"type":        "string",
									"description": "Product identifier",
								},
								"title": map[string]any{
									"type":        "string",
									"description": "Product title",
								},
							},
							"required": []string{"id", "title"},
						},
					},
				},
				"required": []string{"items"},
			},
		},
	}

	result, err := GenerateFromMCPTools(mcpTools)
	if err != nil {
		t.Fatalf("GenerateFromMCPTools returned error: %v", err)
	}

	assertContainsAll(t, result,
		"declare const tools : {",
		"search_products: (input: { limit?: number; query: string; }) => { items: { id: string; title: string; }[]; };",
		"@param limit - Maximum result count",
		"@param query - Search query",
		"@returns items - Matched items",
		"@returns items[].id - Product identifier",
		"@returns items[].title - Product title",
	)
}

func TestToolDefinitionBuilder(t *testing.T) {
	t.Parallel()

	definition := NewToolDefinition("search-products").
		WithDescription("Search the product catalog").
		WithInputSchema(NewObjectSchema().
			WithProperty("query", NewStringSchema().WithDescription("Search query")).
			WithRequired("query")).
		WithOutputSchema(NewArraySchema(NewStringSchema())).
		Build()

	if definition.Name != "search-products" {
		t.Fatalf("Name = %q, want search-products", definition.Name)
	}
	if definition.Description != "Search the product catalog" {
		t.Fatalf("Description = %q, want Search the product catalog", definition.Description)
	}
	if definition.InputSchema == nil {
		t.Fatal("expected input schema")
	}
	if definition.OutputSchema == nil {
		t.Fatal("expected output schema")
	}

	inputSchema := definition.InputSchema.(map[string]any)
	properties := inputSchema["properties"].(map[string]any)
	query := properties["query"].(map[string]any)
	if query["description"] != "Search query" {
		t.Fatalf("query description = %v, want Search query", query["description"])
	}
}

func TestGeneratedDeclarationsCompileWithTypeScript(t *testing.T) {
	t.Parallel()

	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found in PATH")
	}

	declaration, err := GenerateFromDefinitions([]ToolDefinition{
		{
			Name:        "create-project",
			Description: "Create a new project",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []any{"name"},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
				"required": []any{"id"},
			},
		},
		{
			Name: "ping",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []any{},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"ok": map[string]any{"type": "boolean"},
				},
				"required": []any{"ok"},
			},
		},
	})
	if err != nil {
		t.Fatalf("GenerateFromDefinitions returned error: %v", err)
	}

	source := declaration + `

const created = tools.create_project({ name: "demo" });
const id: string = created.id;
const ping = tools.ping();
const ok: boolean = ping.ok;
`

	filePath := filepath.Join(t.TempDir(), "generated.ts")
	if err := os.WriteFile(filePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write TypeScript fixture: %v", err)
	}

	// #nosec G204 -- tsc is resolved by exec.LookPath in this test and the arguments are fixed.
	cmd := exec.Command(tsc, "--noEmit", "--strict", filePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tsc failed: %v\n%s\nsource:\n%s", err, output, source)
	}
}

func TestGenerateFromDefinitionsRequiredArrayDropsNull(t *testing.T) {
	t.Parallel()

	declaration, err := GenerateFromDefinitions([]ToolDefinition{
		{
			Name: "write_data_to_excel",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"data": map[string]any{
						"description": "Array of objects to write",
						"items": map[string]any{
							"additionalProperties": true,
							"type":                 "object",
						},
						"type": []any{"null", "array"},
					},
					"filepath": map[string]any{
						"type": "string",
					},
					"sheet_name": map[string]any{
						"type": "string",
					},
				},
				"required": []any{"filepath", "sheet_name", "data"},
			},
			OutputSchema: map[string]any{
				"type": "object",
			},
		},
	})
	if err != nil {
		t.Fatalf("GenerateFromDefinitions returned error: %v", err)
	}

	assertContainsAll(t, declaration,
		"write_data_to_excel: (input: { data: Record<string, unknown>[]; filepath: string; sheet_name: string; }) => Record<string, unknown>;",
	)
	if strings.Contains(declaration, "data: null |") || strings.Contains(declaration, "data: (null |") {
		t.Fatalf("expected required array property to omit null union\nfull output:\n%s", declaration)
	}
}

func TestSchemaToTypeDeclarationEdges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema any
		want   string
	}{
		{
			name: "nullable string",
			schema: map[string]any{
				"type":     "string",
				"nullable": true,
			},
			want: "string | null",
		},
		{
			name: "empty object allows additional properties by default",
			schema: map[string]any{
				"type": "object",
			},
			want: "Record<string, unknown>",
		},
		{
			name: "additionalProperties false",
			schema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			want: "Record<string, never>",
		},
		{
			name: "additionalProperties schema",
			schema: map[string]any{
				"type": "object",
				"additionalProperties": map[string]any{
					"type": "number",
				},
			},
			want: "Record<string, number>",
		},
		{
			name: "ref",
			schema: map[string]any{
				"$ref": "#/$defs/project",
				"$defs": map[string]any{
					"project": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id": map[string]any{"type": "string"},
						},
						"required": []any{"id"},
					},
				},
			},
			want: "{\n  id: string;\n}",
		},
		{
			name: "oneOf",
			schema: map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "number"},
				},
			},
			want: "string | number",
		},
		{
			name: "anyOf",
			schema: map[string]any{
				"anyOf": []any{
					map[string]any{"type": "boolean"},
					map[string]any{"const": "ready"},
				},
			},
			want: "boolean | \"ready\"",
		},
		{
			name: "enum literals",
			schema: map[string]any{
				"enum": []any{"red", float64(2), false, nil},
			},
			want: "\"red\" | 2 | false | null",
		},
		{
			name: "required array property drops null union",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"data": map[string]any{
						"type": []any{"null", "array"},
						"items": map[string]any{
							"type": "string",
						},
					},
					"label": map[string]any{
						"type": []any{"null", "string"},
					},
				},
				"required": []any{"data"},
			},
			want: "{\n  data: string[];\n  label?: (null | string);\n}",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := schemaToTypeDeclaration(test.schema)
			if got != test.want {
				t.Fatalf("schemaToTypeDeclaration() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestGenerateFromDefinitionsRejectsEmptyName(t *testing.T) {
	t.Parallel()

	_, err := GenerateFromDefinitions([]ToolDefinition{{Name: "   "}})
	if err == nil {
		t.Fatal("expected error for empty tool name")
	}
}

func assertContainsAll(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, fragment := range want {
		if !strings.Contains(got, fragment) {
			t.Fatalf("expected output to contain %q\nfull output:\n%s", fragment, got)
		}
	}
}
