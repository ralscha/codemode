package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"modernc.org/quickjs"
)

const (
	defaultEvalTimeout = 10 * time.Second
	defaultMemoryLimit = uintptr(32 * 1024 * 1024)
)

type ToolCallback func(context.Context, map[string]any) (any, error)

type ToolCallbackDefinition struct {
	Name     string
	Callback ToolCallback
}

type ToolCallbackNamespace struct {
	Namespace string
	Callbacks []ToolCallbackDefinition
}

func NewToolCallback[Input any, Output any](name string, callback func(context.Context, Input) (Output, error)) ToolCallbackDefinition {
	return ToolCallbackDefinition{
		Name: name,
		Callback: func(ctx context.Context, input map[string]any) (any, error) {
			if callback == nil {
				return nil, fmt.Errorf("typed tool callback %q is nil", name)
			}

			var typedInput Input
			encoded, err := json.Marshal(input)
			if err != nil {
				return nil, fmt.Errorf("marshal tool args: %w", err)
			}
			if err := json.Unmarshal(encoded, &typedInput); err != nil {
				return nil, fmt.Errorf("decode tool args: %w", err)
			}

			return callback(ctx, typedInput)
		},
	}
}

type ExecuteResult struct {
	Logs  []string `json:"logs"`
	Value any      `json:"value"`
}

func Execute(ctx context.Context, code string, namespaces []ToolCallbackNamespace, opts ...Option) (result ExecuteResult, err error) {
	resolved := options{
		namespace:   "tools",
		evalTimeout: defaultEvalTimeout,
		memoryLimit: defaultMemoryLimit,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}

	return execute(ctx, code, namespaces, resolved)
}

func execute(ctx context.Context, code string, namespaces []ToolCallbackNamespace, resolved options) (result ExecuteResult, err error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return ExecuteResult{}, fmt.Errorf("create quickjs vm: %w", err)
	}
	defer func() {
		if closeErr := vm.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close quickjs vm: %w", closeErr)
		}
	}()

	vm.SetMemoryLimit(resolved.memoryLimit)
	if err := vm.SetEvalTimeout(resolved.evalTimeout); err != nil {
		return ExecuteResult{}, fmt.Errorf("set eval timeout: %w", err)
	}

	logs := []string{}
	if err := vm.RegisterFunc("__codemode_log", func(payload string) {
		logs = append(logs, payload)
	}, false); err != nil {
		return ExecuteResult{}, fmt.Errorf("register logger: %w", err)
	}

	registered, err := registerToolCallbackNamespaces(ctx, vm, namespaces, resolved.namespace)
	if err != nil {
		return ExecuteResult{}, err
	}

	wrapped := buildExecutePrelude(registered) + "\n(() => {\n" + normalizeCode(code) + "\n})()"
	value, err := vm.Eval(wrapped, quickjs.EvalGlobal)
	if err != nil {
		return ExecuteResult{Logs: logs}, fmt.Errorf("execute javascript: %w", err)
	}

	return ExecuteResult{Logs: logs, Value: value}, nil
}

type registeredToolCallbackNamespace struct {
	namespace string
	callbacks []registeredToolCallback
}

type registeredToolCallback struct {
	methodName string
	bridgeName string
}

func registerToolCallbackNamespaces(ctx context.Context, vm *quickjs.VM, namespaces []ToolCallbackNamespace, defaultNamespace string) ([]registeredToolCallbackNamespace, error) {
	registered := make([]registeredToolCallbackNamespace, 0, len(namespaces))
	usedNamespaces := make(map[string]bool)

	for _, group := range namespaces {
		namespace := sanitizeNamespace(group.Namespace, defaultNamespace)
		if usedNamespaces[namespace] {
			return nil, fmt.Errorf("tool callback namespace %q is duplicated", namespace)
		}
		usedNamespaces[namespace] = true

		callbacks, err := registerToolCallbacks(ctx, vm, namespace, group.Callbacks)
		if err != nil {
			return nil, fmt.Errorf("register namespace %q: %w", namespace, err)
		}
		registered = append(registered, registeredToolCallbackNamespace{namespace: namespace, callbacks: callbacks})
	}

	return registered, nil
}

func registerToolCallbacks(ctx context.Context, vm *quickjs.VM, namespace string, callbacks []ToolCallbackDefinition) ([]registeredToolCallback, error) {
	registered := make([]registeredToolCallback, 0, len(callbacks))
	usedMethodNames := make(map[string]int)

	for index, definition := range callbacks {
		if strings.TrimSpace(definition.Name) == "" {
			return nil, fmt.Errorf("tool callback at index %d has an empty name", index)
		}
		if definition.Callback == nil {
			return nil, fmt.Errorf("tool callback %q is nil", definition.Name)
		}

		methodName := uniqueIdentifier(sanitizeIdentifier(definition.Name), usedMethodNames)
		bridgeName := "__codemode_bridge_" + namespace + "_" + methodName
		callback := definition.Callback

		if err := vm.RegisterFunc(bridgeName, func(payload string) string {
			input, err := decodeCallbackPayload(payload)
			if err != nil {
				return marshalBridgeResponse(nil, fmt.Sprintf("parse tool args: %v", err))
			}

			value, err := callback(ctx, input)
			if err != nil {
				return marshalBridgeResponse(nil, err.Error())
			}
			return marshalBridgeResponse(value, "")
		}, false); err != nil {
			return nil, fmt.Errorf("register %s: %w", bridgeName, err)
		}

		registered = append(registered, registeredToolCallback{methodName: methodName, bridgeName: bridgeName})
	}

	return registered, nil
}

func sanitizeNamespace(namespace string, fallback string) string {
	if strings.TrimSpace(namespace) == "" {
		namespace = fallback
	}
	return sanitizeIdentifier(namespace)
}

const nullPayload = "null"

func decodeCallbackPayload(payload string) (map[string]any, error) {
	if strings.TrimSpace(payload) == "" || strings.TrimSpace(payload) == nullPayload {
		return map[string]any{}, nil
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		return nil, err
	}
	if input == nil {
		return map[string]any{}, nil
	}
	return input, nil
}

func buildExecutePrelude(namespaces []registeredToolCallbackNamespace) string {
	var builder strings.Builder
	builder.WriteString("const console = {\n")
	builder.WriteString("  log: (...args) => __codemode_log(args.map(String).join(' ')),\n")
	builder.WriteString("  warn: (...args) => __codemode_log('[warn] ' + args.map(String).join(' ')),\n")
	builder.WriteString("  error: (...args) => __codemode_log('[error] ' + args.map(String).join(' ')),\n")
	builder.WriteString("};\n")
	for _, namespace := range namespaces {
		fmt.Fprintf(&builder, "const %s = {};\n", namespace.namespace)
		for _, callback := range namespace.callbacks {
			fmt.Fprintf(&builder, "%s.%s = (input) => { const response = JSON.parse(%s(JSON.stringify(input || {}))); if (response.error) { throw new Error(response.error); } return response.value; };\n", namespace.namespace, callback.methodName, callback.bridgeName)
		}
	}
	return builder.String()
}

func marshalBridgeResponse(value any, errText string) string {
	payload := map[string]any{"value": value, "error": errText}
	encoded, err := json.Marshal(payload)
	if err != nil {
		fallback, _ := json.Marshal(map[string]any{"value": nil, "error": fmt.Sprintf("marshal bridge response: %v", err)})
		return string(fallback)
	}
	return string(encoded)
}
