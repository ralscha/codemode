package codemode

import "github.com/modelcontextprotocol/go-sdk/mcp"

type ToolDefinition struct {
	Name         string
	Description  string
	InputSchema  any
	OutputSchema any
}

type ToolDefinitionBuilder struct {
	definition ToolDefinition
}

func NewToolDefinition(name string) ToolDefinitionBuilder {
	return ToolDefinitionBuilder{definition: ToolDefinition{Name: name}}
}

func (builder ToolDefinitionBuilder) WithDescription(description string) ToolDefinitionBuilder {
	builder.definition.Description = description
	return builder
}

func (builder ToolDefinitionBuilder) WithInputSchema(schema any) ToolDefinitionBuilder {
	builder.definition.InputSchema = schemaValue(schema)
	return builder
}

func (builder ToolDefinitionBuilder) WithOutputSchema(schema any) ToolDefinitionBuilder {
	builder.definition.OutputSchema = schemaValue(schema)
	return builder
}

func (builder ToolDefinitionBuilder) Build() ToolDefinition {
	return ToolDefinition{
		Name:         builder.definition.Name,
		Description:  builder.definition.Description,
		InputSchema:  normalizeSchemaValue(builder.definition.InputSchema),
		OutputSchema: normalizeSchemaValue(builder.definition.OutputSchema),
	}
}

type SchemaBuilder struct {
	schema map[string]any
}

func NewObjectSchema() SchemaBuilder {
	return SchemaBuilder{schema: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}}
}

func NewStringSchema() SchemaBuilder {
	return schemaWithType("string")
}

func NewNumberSchema() SchemaBuilder {
	return schemaWithType("number")
}

func NewIntegerSchema() SchemaBuilder {
	return schemaWithType("integer")
}

func NewBooleanSchema() SchemaBuilder {
	return schemaWithType("boolean")
}

func NewArraySchema(items any) SchemaBuilder {
	return SchemaBuilder{schema: map[string]any{
		"type":  "array",
		"items": schemaValue(items),
	}}
}

func (builder SchemaBuilder) WithDescription(description string) SchemaBuilder {
	builder.schema["description"] = description
	return builder
}

func (builder SchemaBuilder) WithProperty(name string, schema any) SchemaBuilder {
	properties, _ := builder.schema["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
		builder.schema["properties"] = properties
	}
	properties[name] = schemaValue(schema)
	return builder
}

func (builder SchemaBuilder) WithRequired(names ...string) SchemaBuilder {
	required := make([]any, 0, len(names))
	for _, name := range names {
		required = append(required, name)
	}
	builder.schema["required"] = required
	return builder
}

func (builder SchemaBuilder) WithEnum(values ...any) SchemaBuilder {
	builder.schema["enum"] = values
	return builder
}

func (builder SchemaBuilder) WithItems(schema any) SchemaBuilder {
	builder.schema["items"] = schemaValue(schema)
	return builder
}

func (builder SchemaBuilder) WithAdditionalProperties(value any) SchemaBuilder {
	builder.schema["additionalProperties"] = schemaValue(value)
	return builder
}

func (builder SchemaBuilder) Build() map[string]any {
	if normalized, ok := normalizeSchemaValue(builder.schema).(map[string]any); ok {
		return normalized
	}
	return nil
}

type ToolIndexDefinition interface {
	ToolDefinition | mcp.Tool
}

func schemaWithType(schemaType string) SchemaBuilder {
	return SchemaBuilder{schema: map[string]any{"type": schemaType}}
}

func schemaValue(schema any) any {
	if builder, ok := schema.(SchemaBuilder); ok {
		return builder.Build()
	}
	return schema
}
