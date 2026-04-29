# codemode

Search tool definitions, generate a TypeScript API surface for them, and execute
small JavaScript programs against caller-provided tool callbacks.

```bash
go get github.com/ralscha/codemode
```

The root package depends directly on [QuickJS](https://gitlab.com/cznic/quickjs) and the [MCP SDK](https://github.com/modelcontextprotocol/go-sdk).

## Tool Search

`NewToolIndex(...)` builds a searchable index over many tool definitions. The
index accepts either `[]codemode.ToolDefinition` or `[]mcp.Tool` and searches
by name, description, input schema, and output schema.

```go
definitions := []codemode.ToolDefinition{
    codemode.NewToolDefinition("search-products").
        WithDescription("Search the product catalog").
        WithInputSchema(codemode.NewObjectSchema().
            WithProperty("query", codemode.NewStringSchema().WithDescription("Search query")).
            WithRequired("query")).
        WithOutputSchema(codemode.NewObjectSchema().
            WithProperty("items", codemode.NewArraySchema(codemode.NewObjectSchema().
                WithProperty("id", codemode.NewStringSchema().WithDescription("Product identifier")).
                WithProperty("title", codemode.NewStringSchema().WithDescription("Product title")).
                WithRequired("id", "title")).WithDescription("Matched items")).
            WithRequired("items")).
        Build(),
}

index, err := codemode.NewToolIndex(definitions)
if err != nil {
    return err
}

results := index.Query("matched product items", codemode.WithToolSearchLimit(3))
for _, result := range results {
    fmt.Println(result.Definition.Name, result.Score, result.Matches)
}
```

`Query(...)` combines BM25-style lexical ranking with exact, normalized, and
fuzzy name boosts. Schema property names and descriptions carry more weight
than raw schema JSON, so queries like `matched product items` can find a tool
from its output shape.

`QueryRegex(...)` provides deterministic filtering:

```go
results, err := index.QueryRegex(`github.*issues`)
```

## Generate API

`GenerateFromDefinitions(...)` and `GenerateFromMCPTools(...)` generate
TypeScript API declarations. The output uses sanitized method names,
generated input and output types, and JSDoc from tool and schema descriptions.

```go
api, err := codemode.GenerateFromDefinitions(definitions)
if err != nil {
    return err
}
fmt.Println(api)
```

The generator produces declarations like:

```ts
declare const tools : {
 /**
  * Create a new project
  * @param name - Project name
  * @returns id - Project identifier
  */
 create_project: (input: { name: string; }) => { id: string; name: string; };
}
```

Supported inputs:

- `GenerateFromDefinitions(...)`: normalized `ToolDefinition` values
- `GenerateFromMCPTools(...)`: MCP SDK `mcp.Tool` values

`ToolDefinition` supports both input and output schemas. The builder APIs keep
nested JSON schema values readable:

```go
definition := codemode.NewToolDefinition("create-project").
    WithDescription("Create a new project").
    WithInputSchema(codemode.NewObjectSchema().
        WithProperty("name", codemode.NewStringSchema().WithDescription("Project name")).
        WithRequired("name")).
    WithOutputSchema(codemode.NewObjectSchema().
        WithProperty("id", codemode.NewStringSchema().WithDescription("Project identifier")).
        WithProperty("name", codemode.NewStringSchema().WithDescription("Project name")).
        WithRequired("id", "name")).
    Build()
```

`GenerateFromMCPTools(...)` works directly with tool definitions from an MCP
server:

```go
api, err := codemode.GenerateFromMCPTools(mcpTools, codemode.WithNamespace("githubTools"))
```

By default the output starts with `declare const tools : { ... }`.
`WithNamespace(...)` changes the object name:

```go
githubAPI, _ := codemode.GenerateFromMCPTools(githubTools, codemode.WithNamespace("githubTools"))
stripeAPI, _ := codemode.GenerateFromMCPTools(stripeTools, codemode.WithNamespace("stripeTools"))
```

## Execute

`Execute(...)` runs JavaScript returned by an LLM. The code executes inside a
QuickJS VM with a memory limit and timeout. The caller provides the callback
namespaces, so execution stays independent of HTTP clients, databases, or any
other tool runtime.

Execution is synchronous. Tool callbacks are exposed as regular JavaScript
functions, so generated code should not use `await`, `Promise.all(...)`, dynamic
`import(...)`, or other async-only patterns.

Before evaluation, code is normalized for common LLM output shapes:

- markdown code fences are stripped
- bare expressions and final expressions are returned automatically
- sync arrow functions and simple `async () => { ... }` wrappers are unwrapped

```go
type WeatherInput struct {
    City string `json:"city"`
}

type WeatherOutput struct {
    City        string `json:"city"`
    Temperature string `json:"temperature"`
}

result, err := codemode.Execute(ctx, `
const weather = tools.get_weather({ city: "Zurich" });
return { text: "Weather in " + weather.city + ": " + weather.temperature };
`, []codemode.ToolCallbackNamespace{
    {
        Callbacks: []codemode.ToolCallbackDefinition{
            codemode.NewToolCallback("get-weather", func(ctx context.Context, input WeatherInput) (WeatherOutput, error) {
                return WeatherOutput{City: input.City, Temperature: "12 C"}, nil
            }),
        },
    },
})
if err != nil {
    return err
}
fmt.Println(result.Value)
```

Sandboxed code can write diagnostic output with `console.log`, `console.warn`,
and `console.error`. Messages are captured in `ExecuteResult.Logs`; warnings and
errors are prefixed with `[warn]` and `[error]`. Logs captured before a
JavaScript error are still returned with the result value.

The JavaScript API uses the same sanitized names as the generated declarations.
For example, `get-weather` becomes `tools.get_weather(...)`.
`WithNamespace(...)` changes the object name for both generation and execution.

```go
api, _ := codemode.GenerateFromDefinitions(definitions, codemode.WithNamespace("sdk"))
result, _ := codemode.Execute(ctx, `return sdk.search_docs({ query: "install" });`, []codemode.ToolCallbackNamespace{
    {Callbacks: callbacks},
}, codemode.WithNamespace("sdk"))
```

Multiple namespace entries expose helper objects from multiple generated
namespaces to the same JavaScript program.

```go
result, err := codemode.Execute(ctx, `
const repos = githubTools.list_repos({ owner: "ralscha" }).items;
const customers = stripeTools.list_customers({ limit: 2 }).items;
return { repos: repos.length, customers: customers.length };
`, []codemode.ToolCallbackNamespace{
    {Namespace: "githubTools", Callbacks: githubCallbacks},
    {Namespace: "stripeTools", Callbacks: stripeCallbacks},
})
```

Execution options:

- `WithEvalTimeout(...)`: limit JavaScript runtime, default `10s`
- `WithMemoryLimit(...)`: limit QuickJS memory, default `32 MiB`

## License

MIT License. See [LICENSE](LICENSE) for details.
