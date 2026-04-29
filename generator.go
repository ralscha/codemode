package codemode

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Option func(*options)

type options struct {
	namespace   string
	evalTimeout time.Duration
	memoryLimit uintptr
}

func WithNamespace(namespace string) Option {
	return func(opts *options) {
		namespace = strings.TrimSpace(namespace)
		if namespace != "" {
			opts.namespace = sanitizeIdentifier(namespace)
		}
	}
}

func WithEvalTimeout(timeout time.Duration) Option {
	return func(opts *options) {
		if timeout > 0 {
			opts.evalTimeout = timeout
		}
	}
}

func WithMemoryLimit(bytes uintptr) Option {
	return func(opts *options) {
		if bytes > 0 {
			opts.memoryLimit = bytes
		}
	}
}

func GenerateFromMCPTools(tools []mcp.Tool, opts ...Option) (string, error) {
	definitions := make([]ToolDefinition, 0, len(tools))
	for index, item := range tools {
		definition, err := definitionFromMCPTool(item)
		if err != nil {
			return "", fmt.Errorf("convert mcp tool at index %d: %w", index, err)
		}

		definitions = append(definitions, definition)
	}

	return GenerateFromDefinitions(definitions, opts...)
}

func GenerateFromDefinitions(definitions []ToolDefinition, opts ...Option) (string, error) {
	resolved := options{namespace: "tools"}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}

	entries := make([]renderedTool, 0, len(definitions))
	usedMethodNames := make(map[string]int)
	usedTypeNames := make(map[string]int)

	for index, definition := range definitions {
		if strings.TrimSpace(definition.Name) == "" {
			return "", fmt.Errorf("tool at index %d has an empty name", index)
		}

		methodName := uniqueIdentifier(sanitizeIdentifier(definition.Name), usedMethodNames)
		typeStem := uniqueTypeName(toPascalCase(methodName), usedTypeNames)

		inputType := schemaToTypeDeclaration(definition.InputSchema)
		outputType := schemaToTypeDeclaration(definition.OutputSchema)

		entries = append(entries, renderedTool{
			originalName: definition.Name,
			methodName:   methodName,
			typeStem:     typeStem,
			description:  definition.Description,
			inputType:    inputType,
			outputType:   outputType,
			inputSchema:  normalizeSchemaValue(definition.InputSchema),
			outputSchema: normalizeSchemaValue(definition.OutputSchema),
		})
	}

	return renderDefinitions(entries, resolved.namespace), nil
}

type renderedTool struct {
	originalName string
	methodName   string
	typeStem     string
	description  string
	inputType    string
	outputType   string
	inputSchema  any
	outputSchema any
}

func definitionFromMCPTool(value any) (ToolDefinition, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ToolDefinition{}, fmt.Errorf("marshal mcp tool: %w", err)
	}

	var envelope struct {
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		InputSchema  json.RawMessage `json:"inputSchema"`
		OutputSchema json.RawMessage `json:"outputSchema"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return ToolDefinition{}, fmt.Errorf("decode mcp tool envelope: %w", err)
	}

	if strings.TrimSpace(envelope.Name) == "" {
		return ToolDefinition{}, errors.New("mcp tool is missing name")
	}

	return ToolDefinition{
		Name:         envelope.Name,
		Description:  envelope.Description,
		InputSchema:  normalizeSchemaBytes(envelope.InputSchema),
		OutputSchema: normalizeSchemaBytes(envelope.OutputSchema),
	}, nil
}

func normalizeSchemaBytes(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}

	return normalizeSchemaValue(value)
}

func normalizeSchemaValue(value any) any {
	if value == nil {
		return nil
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil
	}

	return normalized
}

func sanitizeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "tool"
	}

	var builder strings.Builder
	for index, char := range value {
		if unicode.IsLetter(char) || char == '_' || (index > 0 && unicode.IsDigit(char)) {
			builder.WriteRune(char)
			continue
		}
		builder.WriteByte('_')
	}

	identifier := builder.String()
	if identifier == "" {
		identifier = "tool"
	}
	if unicode.IsDigit(rune(identifier[0])) {
		identifier = "_" + identifier
	}
	if tsKeywords[identifier] {
		identifier += "_"
	}

	return identifier
}

func uniqueIdentifier(base string, used map[string]int) string {
	if used[base] == 0 {
		used[base] = 1
		return base
	}
	used[base]++
	return fmt.Sprintf("%s_%d", base, used[base])
}

func uniqueTypeName(base string, used map[string]int) string {
	if base == "" {
		base = "Tool"
	}
	if used[base] == 0 {
		used[base] = 1
		return base
	}
	used[base]++
	return fmt.Sprintf("%s%d", base, used[base])
}

func toPascalCase(value string) string {
	parts := strings.FieldsFunc(value, func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsDigit(char)
	})
	if len(parts) == 0 {
		return "Tool"
	}

	var builder strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		builder.WriteRune(unicode.ToUpper(runes[0]))
		for _, char := range runes[1:] {
			builder.WriteRune(char)
		}
	}

	result := builder.String()
	if result == "" {
		return "Tool"
	}
	if unicode.IsDigit(rune(result[0])) {
		return "T" + result
	}
	return result
}

func renderDefinitions(entries []renderedTool, namespace string) string {
	if len(entries) == 0 {
		return fmt.Sprintf("declare const %s : {}", namespace)
	}

	apiLines := make([]string, 0, len(entries)*6)

	for _, entry := range entries {
		docLines := buildDocLines(entry)
		apiLines = append(apiLines, " /**")
		for _, line := range docLines {
			apiLines = append(apiLines, "  * "+line)
		}
		apiLines = append(apiLines, " */")
		signature := fmt.Sprintf("%s: %s;", entry.methodName, toolFunctionType(entry))
		apiLines = append(apiLines, indentLines(signature, " "))
		apiLines = append(apiLines, "")
	}

	if len(apiLines) > 0 && apiLines[len(apiLines)-1] == "" {
		apiLines = apiLines[:len(apiLines)-1]
	}

	return strings.TrimSpace(fmt.Sprintf("declare const %s : {\n%s\n}", namespace, strings.Join(apiLines, "\n")))
}

func toolFunctionType(entry renderedTool) string {
	outputType := compactTypeString(entry.outputType)
	if isNoInputSchema(entry.inputSchema) {
		return fmt.Sprintf("() => %s", outputType)
	}
	return fmt.Sprintf("(input: %s) => %s", compactTypeString(entry.inputType), outputType)
}

func isNoInputSchema(schemaValue any) bool {
	schemaMap, ok := schemaValue.(map[string]any)
	if !ok {
		return false
	}

	if schemaType, _ := schemaMap["type"].(string); schemaType != "" && schemaType != "object" {
		return false
	}

	properties, _ := schemaMap["properties"].(map[string]any)
	if len(properties) > 0 {
		return false
	}

	required, _ := schemaMap["required"].([]any)
	if len(required) > 0 {
		return false
	}

	additional, hasAdditional := schemaMap["additionalProperties"]
	if !hasAdditional {
		return true
	}
	allowed, ok := additional.(bool)
	return ok && !allowed
}

func indentLines(value string, prefix string) string {
	parts := strings.Split(value, "\n")
	for index, part := range parts {
		parts[index] = prefix + part
	}
	return strings.Join(parts, "\n")
}

func compactTypeString(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func buildDocLines(entry renderedTool) []string {
	lines := make([]string, 0, 4)
	description := strings.TrimSpace(entry.description)
	if description == "" {
		lines = append(lines, escapeDoc(entry.originalName))
	} else {
		lines = append(lines, escapeDoc(oneLine(description)))
	}

	for _, paramLine := range schemaParamDocLines(entry.inputSchema) {
		lines = append(lines, escapeDoc(paramLine))
	}
	for _, outputLine := range schemaReturnDocLines(entry.outputSchema) {
		lines = append(lines, escapeDoc(outputLine))
	}

	return lines
}

func schemaParamDocLines(schemaValue any) []string {
	schemaMap, ok := schemaValue.(map[string]any)
	if !ok {
		return nil
	}

	properties, _ := schemaMap["properties"].(map[string]any)
	if len(properties) == 0 {
		return nil
	}

	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		property, _ := properties[key].(map[string]any)
		description, _ := property["description"].(string)
		if strings.TrimSpace(description) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("@param %s - %s", key, oneLine(description)))
	}

	return lines
}

func schemaReturnDocLines(schemaValue any) []string {
	root, ok := schemaValue.(map[string]any)
	if !ok {
		return nil
	}

	var lines []string
	collectSchemaDocLines(root, "", "@returns", &lines)
	return lines
}

func collectSchemaDocLines(schemaValue any, path string, tag string, lines *[]string) {
	schemaMap, ok := schemaValue.(map[string]any)
	if !ok {
		return
	}

	if path != "" {
		description, _ := schemaMap["description"].(string)
		if strings.TrimSpace(description) != "" {
			*lines = append(*lines, fmt.Sprintf("%s %s - %s", tag, path, oneLine(description)))
		}
	}

	properties, _ := schemaMap["properties"].(map[string]any)
	if len(properties) > 0 {
		keys := make([]string, 0, len(properties))
		for key := range properties {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			collectSchemaDocLines(properties[key], childPath, tag, lines)
		}
	}

	if items, ok := schemaMap["items"].(map[string]any); ok {
		childPath := path + "[]"
		if path == "" {
			childPath = "[]"
		}
		collectSchemaDocLines(items, childPath, tag, lines)
	}
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\r", " ")), " ")
}

func escapeDoc(value string) string {
	return strings.ReplaceAll(value, "*/", "*\\/")
}

var tsKeywords = map[string]bool{
	"break":      true,
	"case":       true,
	"catch":      true,
	"class":      true,
	"const":      true,
	"continue":   true,
	"debugger":   true,
	"default":    true,
	"delete":     true,
	"do":         true,
	"else":       true,
	"enum":       true,
	"export":     true,
	"extends":    true,
	"false":      true,
	"finally":    true,
	"for":        true,
	"function":   true,
	"if":         true,
	"import":     true,
	"in":         true,
	"instanceof": true,
	"new":        true,
	"null":       true,
	"return":     true,
	"super":      true,
	"switch":     true,
	"this":       true,
	"throw":      true,
	"true":       true,
	"try":        true,
	"typeof":     true,
	"var":        true,
	"void":       true,
	"while":      true,
	"with":       true,
	"yield":      true,
}
