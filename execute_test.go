package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"modernc.org/quickjs"
)

func TestExecuteCallsToolCallbacks(t *testing.T) {
	t.Parallel()

	var received map[string]any
	result, err := Execute(context.Background(), `
const added = tools.add_numbers({ a: 2, b: 3 });
console.log("sum", added.sum);
return { answer: added.sum };
`, defaultNamespace(
		ToolCallbackDefinition{
			Name: "add-numbers",
			Callback: func(_ context.Context, input map[string]any) (any, error) {
				received = input
				return map[string]any{"sum": input["a"].(float64) + input["b"].(float64)}, nil
			},
		},
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if received["a"] != float64(2) || received["b"] != float64(3) {
		t.Fatalf("callback received %#v", received)
	}
	assertJSONEqual(t, result.Value, map[string]any{"answer": float64(5)})
	if len(result.Logs) != 1 || result.Logs[0] != `sum 5` {
		t.Fatalf("logs = %#v, want sum log", result.Logs)
	}
}

func TestExecuteRejectsNonObjectToolInput(t *testing.T) {
	t.Parallel()

	_, err := Execute(context.Background(), `return tools.inspect(false);`, defaultNamespace(
		ToolCallbackDefinition{
			Name: "inspect",
			Callback: func(context.Context, map[string]any) (any, error) {
				t.Fatal("callback should not be called with non-object input")
				return nil, nil
			},
		},
	))
	if err == nil || !strings.Contains(err.Error(), "parse tool args") {
		t.Fatalf("err = %v, want parse tool args error", err)
	}
}

func TestExecuteCapturesConsoleOutput(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `
console.log("hello from sandbox");
console.warn("watch out");
console.error("bad thing");
return "done";
`, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	want := []string{"hello from sandbox", "[warn] watch out", "[error] bad thing"}
	if !reflect.DeepEqual(result.Logs, want) {
		t.Fatalf("logs = %#v, want %#v", result.Logs, want)
	}
	if result.Value != "done" {
		t.Fatalf("result.Value = %#v, want done", result.Value)
	}
}

func TestExecuteReturnsLogsWithExecutionError(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `
console.log("before error");
throw new Error("boom");
`, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want boom", err)
	}
	if !reflect.DeepEqual(result.Logs, []string{"before error"}) {
		t.Fatalf("logs = %#v, want before error", result.Logs)
	}
}

func TestExecuteUsesNamespaceAndSanitizedNames(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `return sdk.class_();`, defaultNamespace(
		ToolCallbackDefinition{
			Name: "class",
			Callback: func(context.Context, map[string]any) (any, error) {
				return "ok", nil
			},
		},
	), WithNamespace("sdk"))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Value != "ok" {
		t.Fatalf("result.Value = %#v, want ok", result.Value)
	}
}

func TestExecuteWithMultipleNamespaces(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `
const repos = githubTools.list({ owner: "ralscha" }).items;
const customers = stripeTools.list({ limit: 2 }).items;
return { repo_count: repos.length, customer_count: customers.length };
`, []ToolCallbackNamespace{
		{
			Namespace: "githubTools",
			Callbacks: []ToolCallbackDefinition{
				{
					Name: "list",
					Callback: func(context.Context, map[string]any) (any, error) {
						return map[string]any{"items": []any{"codemode", "blog2025"}}, nil
					},
				},
			},
		},
		{
			Namespace: "stripeTools",
			Callbacks: []ToolCallbackDefinition{
				{
					Name: "list",
					Callback: func(context.Context, map[string]any) (any, error) {
						return map[string]any{"items": []any{"cus_1", "cus_2", "cus_3"}}, nil
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	assertJSONEqual(t, result.Value, map[string]any{"repo_count": 2, "customer_count": 3})
}

func TestExecuteRejectsDuplicateNamespace(t *testing.T) {
	t.Parallel()

	_, err := Execute(context.Background(), `return null;`, []ToolCallbackNamespace{
		{Namespace: "github-tools"},
		{Namespace: "github_tools"},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("err = %v, want duplicated namespace error", err)
	}
}

func TestExecuteUsesDefaultNamespaceFallback(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `return sdk.ping();`, []ToolCallbackNamespace{
		{
			Callbacks: []ToolCallbackDefinition{
				{
					Name: "ping",
					Callback: func(context.Context, map[string]any) (any, error) {
						return true, nil
					},
				},
			},
		},
	}, WithNamespace("sdk"))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Value != true {
		t.Fatalf("result.Value = %#v, want true", result.Value)
	}
}

func TestExecuteSupportsTypedCallbacks(t *testing.T) {
	t.Parallel()

	type addInput struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	type addOutput struct {
		Sum float64 `json:"sum"`
	}

	result, err := Execute(context.Background(), `return tools.add_numbers({ a: 4, b: 6 }).sum;`, defaultNamespace(
		NewToolCallback("add-numbers", func(_ context.Context, input addInput) (addOutput, error) {
			return addOutput{Sum: input.A + input.B}, nil
		}),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	assertJSONEqual(t, result.Value, 10)
}

func TestExecutePropagatesCallbackErrors(t *testing.T) {
	t.Parallel()

	_, err := Execute(context.Background(), `return tools.fail({});`, defaultNamespace(
		ToolCallbackDefinition{
			Name: "fail",
			Callback: func(context.Context, map[string]any) (any, error) {
				return nil, errors.New("tool failed")
			},
		},
	))
	if err == nil || !strings.Contains(err.Error(), "tool failed") {
		t.Fatalf("err = %v, want tool failed", err)
	}
}

func TestExecuteRejectsInvalidCallbacks(t *testing.T) {
	t.Parallel()

	_, err := Execute(context.Background(), `return null;`, defaultNamespace(ToolCallbackDefinition{Name: "   "}))
	if err == nil {
		t.Fatal("expected empty callback name error")
	}

	_, err = Execute(context.Background(), `return null;`, defaultNamespace(ToolCallbackDefinition{Name: "missing"}))
	if err == nil {
		t.Fatal("expected nil callback error")
	}
}

func TestExecuteHonorsEvalTimeout(t *testing.T) {
	t.Parallel()

	_, err := Execute(context.Background(), `while (true) {}`, nil, WithEvalTimeout(10*time.Millisecond))
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestExecuteNormalizesBareExpression(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `1 + 2`, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	assertJSONEqual(t, result.Value, 3)
}

func TestExecuteNormalizesFinalExpression(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `
const weather = tools.get_weather({ city: "Zurich" });
({ text: "Weather in " + weather.city + ": " + weather.temperature });
`, defaultNamespace(
		NewToolCallback("get-weather", func(_ context.Context, input struct {
			City string `json:"city"`
		}) (map[string]string, error) {
			return map[string]string{"city": input.City, "temperature": "12 C"}, nil
		}),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	assertJSONEqual(t, result.Value, map[string]any{"text": "Weather in Zurich: 12 C"})
}

func TestExecuteNormalizesMarkdownFence(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), "```js\nconst x = 4;\nx * 2\n```", nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	assertJSONEqual(t, result.Value, 8)
}

func TestExecuteNormalizesArrowFunction(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `() => tools.ping({})`, defaultNamespace(
		NewToolCallback("ping", func(context.Context, map[string]any) (string, error) {
			return "pong", nil
		}),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Value != "pong" {
		t.Fatalf("result.Value = %#v, want pong", result.Value)
	}
}

func TestExecuteNormalizesAsyncArrowWithoutAwait(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `async () => { return 42; }`, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	assertJSONEqual(t, result.Value, 42)
}

func TestExecuteNormalizesParenthesizedArrowFunction(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `(async () => { return 42; })`, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	assertJSONEqual(t, result.Value, 42)
}

func TestExecuteDoesNotReturnDeclarations(t *testing.T) {
	t.Parallel()

	result, err := Execute(context.Background(), `const x = 1; const y = 2;`, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if _, ok := result.Value.(quickjs.Undefined); !ok {
		t.Fatalf("result.Value = %#v, want quickjs.Undefined", result.Value)
	}
}

func defaultNamespace(callbacks ...ToolCallbackDefinition) []ToolCallbackNamespace {
	return []ToolCallbackNamespace{{Callbacks: callbacks}}
}

func assertJSONEqual(t *testing.T, got any, want any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}

	var gotNormalized any
	if err := json.Unmarshal(gotJSON, &gotNormalized); err != nil {
		t.Fatalf("normalize got: %v", err)
	}
	var wantNormalized any
	if err := json.Unmarshal(wantJSON, &wantNormalized); err != nil {
		t.Fatalf("normalize want: %v", err)
	}

	if !reflect.DeepEqual(gotNormalized, wantNormalized) {
		t.Fatalf("got %s, want %s", gotJSON, wantJSON)
	}
}
