package codemode

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolIndexQueryRanksByNameAndDescription(t *testing.T) {
	t.Parallel()

	index := mustToolIndex(t, []ToolDefinition{
		{
			Name:        "get-weather",
			Description: "Get the current weather for a city",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string", "description": "City name"},
				},
			},
		},
		{
			Name:        "create-project",
			Description: "Create a new project workspace",
		},
	})

	results := index.Query("weather city")
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	if results[0].Definition.Name != "get-weather" {
		t.Fatalf("top result = %q, want get-weather", results[0].Definition.Name)
	}
	if results[0].Score <= 0 {
		t.Fatalf("score = %v, want positive", results[0].Score)
	}
	if len(results[0].Matches) == 0 {
		t.Fatal("expected match snippets")
	}
}

func TestToolIndexQueryUsesSchemaFields(t *testing.T) {
	t.Parallel()

	index := mustToolIndex(t, []ToolDefinition{
		{
			Name:        "search-products",
			Description: "Search the product catalog",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"limit": map[string]any{"type": "number", "description": "Maximum result count"},
				},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{"type": "array", "description": "Matched product items"},
				},
			},
		},
		{
			Name:        "delete-user",
			Description: "Remove a user account",
		},
	})

	results := index.Query("matched product items")
	if len(results) == 0 {
		t.Fatal("expected schema search results")
	}
	if results[0].Definition.Name != "search-products" {
		t.Fatalf("top result = %q, want search-products", results[0].Definition.Name)
	}
}

func TestToolIndexAcceptsMCPTools(t *testing.T) {
	t.Parallel()

	index, err := NewToolIndex([]mcp.Tool{
		{
			Name:        "search-products",
			Description: "Search the product catalog",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query"},
				},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{"type": "array", "description": "Matched product items"},
				},
			},
		},
		{
			Name:        "delete-user",
			Description: "Remove a user account",
		},
	})
	if err != nil {
		t.Fatalf("NewToolIndex returned error: %v", err)
	}

	results := index.Query("matched product items")
	if len(results) == 0 {
		t.Fatal("expected MCP search results")
	}
	if results[0].Definition.Name != "search-products" {
		t.Fatalf("top result = %q, want search-products", results[0].Definition.Name)
	}
}

func TestToolIndexQueryHandlesFuzzyToolNames(t *testing.T) {
	t.Parallel()

	index := mustToolIndex(t, []ToolDefinition{
		{Name: "list-repositories", Description: "List source repositories"},
		{Name: "send-email", Description: "Send an email message"},
	})

	results := index.Query("list repositorie")
	if len(results) == 0 {
		t.Fatal("expected fuzzy search results")
	}
	if results[0].Definition.Name != "list-repositories" {
		t.Fatalf("top result = %q, want list-repositories", results[0].Definition.Name)
	}
}

func TestToolIndexQueryRegex(t *testing.T) {
	t.Parallel()

	index := mustToolIndex(t, []ToolDefinition{
		{Name: "github-list-issues", Description: "List GitHub issues"},
		{Name: "slack-post-message", Description: "Post a Slack message"},
	})

	results, err := index.QueryRegex(`github.*issues`)
	if err != nil {
		t.Fatalf("QueryRegex returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Definition.Name != "github-list-issues" {
		t.Fatalf("result = %q, want github-list-issues", results[0].Definition.Name)
	}
}

func TestToolIndexQueryOptions(t *testing.T) {
	t.Parallel()

	index := mustToolIndex(t, []ToolDefinition{
		{Name: "search-docs", Description: "Search documentation"},
		{Name: "search-code", Description: "Search source code"},
		{Name: "create-project", Description: "Create a project"},
	})

	results := index.Query("search", WithToolSearchLimit(1), WithToolSearchMinScore(0.1))
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if !strings.HasPrefix(results[0].Definition.Name, "search-") {
		t.Fatalf("result = %q, want search tool", results[0].Definition.Name)
	}
}

func TestNewToolIndexRejectsEmptyNames(t *testing.T) {
	t.Parallel()

	_, err := NewToolIndex([]ToolDefinition{{Name: "   "}})
	if err == nil || !strings.Contains(err.Error(), "empty name") {
		t.Fatalf("err = %v, want empty name error", err)
	}
}

func TestToolIndexQueryRegexRejectsInvalidPattern(t *testing.T) {
	t.Parallel()

	index := mustToolIndex(t, []ToolDefinition{{Name: "search-docs"}})
	_, err := index.QueryRegex("[")
	if err == nil {
		t.Fatal("expected invalid regex error")
	}
}

func mustToolIndex(t *testing.T, definitions []ToolDefinition) *ToolIndex {
	t.Helper()

	index, err := NewToolIndex(definitions)
	if err != nil {
		t.Fatalf("NewToolIndex returned error: %v", err)
	}
	return index
}
