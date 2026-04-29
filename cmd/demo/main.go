package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ralscha/codemode"
)

func main() {
	ctx := context.Background()

	searchDefinitions := []codemode.ToolDefinition{
		codemode.NewToolDefinition("search-products").
			WithDescription("Search the product catalog").
			WithInputSchema(codemode.NewObjectSchema().
				WithProperty("query", codemode.NewStringSchema().WithDescription("Search query")).
				WithProperty("limit", codemode.NewNumberSchema().WithDescription("Maximum number of results")).
				WithRequired("query")).
			WithOutputSchema(codemode.NewObjectSchema().
				WithProperty("items", codemode.NewArraySchema(codemode.NewObjectSchema().
					WithProperty("id", codemode.NewStringSchema().WithDescription("Product identifier")).
					WithProperty("title", codemode.NewStringSchema().WithDescription("Product title")).
					WithRequired("id", "title")).WithDescription("Matched items")).
				WithRequired("items")).
			Build(),
		codemode.NewToolDefinition("github-list-issues").
			WithDescription("List GitHub issues for a repository").
			WithInputSchema(codemode.NewObjectSchema().
				WithProperty("owner", codemode.NewStringSchema().WithDescription("Repository owner or organization")).
				WithProperty("repo", codemode.NewStringSchema().WithDescription("Repository name")).
				WithProperty("state", codemode.NewStringSchema().WithDescription("Issue state filter").WithEnum("open", "closed", "all")).
				WithRequired("owner", "repo")).
			WithOutputSchema(codemode.NewObjectSchema().
				WithProperty("items", codemode.NewArraySchema(codemode.NewObjectSchema().
					WithProperty("number", codemode.NewNumberSchema().WithDescription("Issue number")).
					WithProperty("title", codemode.NewStringSchema().WithDescription("Issue title")).
					WithRequired("number", "title")).WithDescription("Matched issues")).
				WithRequired("items")).
			Build(),
		codemode.NewToolDefinition("slack-post-message").
			WithDescription("Post a message to a Slack channel").
			WithInputSchema(codemode.NewObjectSchema().
				WithProperty("channel", codemode.NewStringSchema().WithDescription("Slack channel name")).
				WithProperty("text", codemode.NewStringSchema().WithDescription("Message text")).
				WithRequired("channel", "text")).
			Build(),
	}

	printSection("Example 1: Search tool definitions")
	toolIndex, err := codemode.NewToolIndex(searchDefinitions)
	if err != nil {
		log.Fatalf("tool search example failed: %v", err)
	}
	printSearchResults("query: repository issues", toolIndex.Query("repository issues", codemode.WithToolSearchLimit(2)))
	regexResults, err := toolIndex.QueryRegex(`product.*items`, codemode.WithToolSearchLimit(2))
	if err != nil {
		log.Fatalf("tool regex search example failed: %v", err)
	}
	printSearchResults("regex: product.*items", regexResults)

	mcpTools := []mcp.Tool{
		{
			Name:         searchDefinitions[0].Name,
			Description:  searchDefinitions[0].Description,
			InputSchema:  searchDefinitions[0].InputSchema,
			OutputSchema: searchDefinitions[0].OutputSchema,
		},
	}

	printSection("Example 1b: Search MCP tools")
	mcpIndex, err := codemode.NewToolIndex(mcpTools)
	if err != nil {
		log.Fatalf("mcp tool search example failed: %v", err)
	}
	printSearchResults("query: product catalog", mcpIndex.Query("product catalog", codemode.WithToolSearchLimit(2)))

	printSection("Example 2: Generate API from ToolDefinition")
	definitionExample, err := codemode.GenerateFromDefinitions([]codemode.ToolDefinition{
		codemode.NewToolDefinition("create-project").
			WithDescription("Create a new project").
			WithInputSchema(codemode.NewObjectSchema().
				WithProperty("name", codemode.NewStringSchema().WithDescription("Project name")).
				WithRequired("name")).
			WithOutputSchema(codemode.NewObjectSchema().
				WithProperty("id", codemode.NewStringSchema().WithDescription("Project identifier")).
				WithProperty("name", codemode.NewStringSchema().WithDescription("Project name")).
				WithRequired("id", "name")).
			Build(),
	})
	if err != nil {
		log.Fatalf("tool definition generation example failed: %v", err)
	}
	fmt.Println(definitionExample)

	printSection("Example 2b: Generate API from MCP tools")
	mcpExample, err := codemode.GenerateFromMCPTools(mcpTools)
	if err != nil {
		log.Fatalf("mcp generation example failed: %v", err)
	}
	fmt.Println(mcpExample)

	printSection("Example 2c: Generate API from ToolDefinition with output schema")
	baseToolWithOutputExample, err := codemode.GenerateFromDefinitions([]codemode.ToolDefinition{
		codemode.NewToolDefinition("search-docs").
			WithDescription("Search internal documentation").
			WithInputSchema(codemode.NewObjectSchema().
				WithProperty("query", codemode.NewStringSchema().WithDescription("Search query")).
				WithProperty("limit", codemode.NewIntegerSchema().WithDescription("Maximum number of matches")).
				WithRequired("query")).
			WithOutputSchema(codemode.NewArraySchema(codemode.NewObjectSchema().
				WithProperty("title", codemode.NewStringSchema().WithDescription("Document title")).
				WithProperty("path", codemode.NewStringSchema().WithDescription("Document path")).
				WithRequired("title", "path"))).
			Build(),
	})
	if err != nil {
		log.Fatalf("tool definition with output generation example failed: %v", err)
	}
	fmt.Println(baseToolWithOutputExample)

	printSection("Example 2d: Generate APIs for multiple namespaces")
	githubTools := []mcp.Tool{
		{
			Name:        "list-repos",
			Description: "List GitHub repositories",
			InputSchema: codemode.NewObjectSchema().
				WithProperty("owner", codemode.NewStringSchema().WithDescription("Repository owner or organization")).
				WithRequired("owner").
				Build(),
		},
	}

	githubExample, err := codemode.GenerateFromMCPTools(githubTools, codemode.WithNamespace("githubTools"))
	if err != nil {
		log.Fatalf("github mcp example failed: %v", err)
	}
	fmt.Println(githubExample)

	stripeTools := []mcp.Tool{
		{
			Name:        "list-customers",
			Description: "List Stripe customers",
			InputSchema: codemode.NewObjectSchema().
				WithProperty("limit", codemode.NewNumberSchema().WithDescription("Maximum number of customers to return")).
				Build(),
		},
	}

	stripeExample, err := codemode.GenerateFromMCPTools(stripeTools, codemode.WithNamespace("stripeTools"))
	if err != nil {
		log.Fatalf("stripe mcp example failed: %v", err)
	}
	fmt.Println(stripeExample)

	printSection("Example 3: Execute JavaScript with a typed callback")
	weatherResult, err := codemode.Execute(ctx, `
const weather = tools.get_weather({ city: "Zurich" });
console.log("resolved", weather.city);
return { message: "Weather in " + weather.city + ": " + weather.temperature_c + " C" };
`, []codemode.ToolCallbackNamespace{
		{
			Callbacks: []codemode.ToolCallbackDefinition{
				codemode.NewToolCallback("get-weather", func(_ context.Context, input weatherInput) (weatherOutput, error) {
					return weatherOutput{
						City:         input.City,
						TemperatureC: 12.5,
						Condition:    "cloudy",
					}, nil
				}),
			},
		},
	})
	if err != nil {
		log.Fatalf("execute weather example failed: %v", err)
	}
	printExecuteResult(weatherResult)

	printSection("Example 3b: Execute JavaScript orchestration with multiple namespaces")
	shoppingResult, err := codemode.Execute(ctx, `
const products = catalogTools.list_products({ query: "keyboard", max_price: 100 }).items;
const priced = products.map((item) => {
	const discount = pricingTools.quote_discount({ price: item.price, percent: item.clearance ? 15 : 5 });
  return {
    sku: item.sku,
    title: item.title,
    final_price: discount.final_price,
  };
});
priced.sort((a, b) => a.final_price - b.final_price);
console.log("candidates", priced.length);
return priced[0] || null;
`, []codemode.ToolCallbackNamespace{
		{
			Namespace: "catalogTools",
			Callbacks: []codemode.ToolCallbackDefinition{
				codemode.NewToolCallback("list-products", listProducts),
			},
		},
		{
			Namespace: "pricingTools",
			Callbacks: []codemode.ToolCallbackDefinition{
				codemode.NewToolCallback("quote-discount", quoteDiscount),
			},
		},
	})
	if err != nil {
		log.Fatalf("execute orchestration example failed: %v", err)
	}
	printExecuteResult(shoppingResult)
}

type weatherInput struct {
	City string `json:"city"`
}

type weatherOutput struct {
	City         string  `json:"city"`
	TemperatureC float64 `json:"temperature_c"`
	Condition    string  `json:"condition"`
}

type listProductsInput struct {
	Query    string  `json:"query"`
	MaxPrice float64 `json:"max_price"`
}

type listProductsOutput struct {
	Items []product `json:"items"`
}

type product struct {
	SKU       string  `json:"sku"`
	Title     string  `json:"title"`
	Price     float64 `json:"price"`
	Clearance bool    `json:"clearance"`
}

type quoteDiscountInput struct {
	Price   float64 `json:"price"`
	Percent float64 `json:"percent"`
}

type quoteDiscountOutput struct {
	FinalPrice float64 `json:"final_price"`
}

func listProducts(_ context.Context, input listProductsInput) (listProductsOutput, error) {
	catalog := []product{
		{SKU: "kb-compact", Title: "Compact Keyboard", Price: 79, Clearance: false},
		{SKU: "kb-pro", Title: "Pro Keyboard", Price: 129, Clearance: true},
		{SKU: "kb-travel", Title: "Travel Keyboard", Price: 59, Clearance: true},
	}

	items := make([]product, 0, len(catalog))
	for _, item := range catalog {
		if item.Price <= input.MaxPrice && strings.Contains(strings.ToLower(item.Title), strings.ToLower(input.Query)) {
			items = append(items, item)
		}
	}

	return listProductsOutput{Items: items}, nil
}

func quoteDiscount(_ context.Context, input quoteDiscountInput) (quoteDiscountOutput, error) {
	return quoteDiscountOutput{FinalPrice: input.Price * (1 - input.Percent/100)}, nil
}

func printSection(title string) {
	line := strings.Repeat("=", len(title))
	fmt.Println(line)
	fmt.Println(title)
	fmt.Println(line)
}

func printExecuteResult(result codemode.ExecuteResult) {
	if len(result.Logs) > 0 {
		fmt.Println("logs:")
		for _, line := range result.Logs {
			fmt.Println("  " + line)
		}
	}

	encoded, err := json.MarshalIndent(result.Value, "", "  ")
	if err != nil {
		fmt.Printf("value: %#v\n", result.Value)
		return
	}
	fmt.Printf("value:\n%s\n", encoded)
}

func printSearchResults(label string, results []codemode.ToolSearchResult) {
	fmt.Println(label)
	for _, result := range results {
		fmt.Printf("  %s (score %.2f)\n", result.Definition.Name, result.Score)
		for _, match := range result.Matches {
			fmt.Println("    - " + match)
		}
	}
}
