package api

type Spec struct {
	OpenAPI    string                `json:"openapi"`
	Info       Info                  `json:"info"`
	Servers    []Server              `json:"servers,omitempty"`
	Paths      map[string]PathItem   `json:"paths"`
	Components Components            `json:"components"`
	Security   []SecurityRequirement `json:"security,omitempty"`
}

type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Server struct {
	URL string `json:"url"`
}

type PathItem struct {
	Parameters []Parameter `json:"parameters,omitempty"`
	Get        *Operation  `json:"get,omitempty"`
	Post       *Operation  `json:"post,omitempty"`
	Put        *Operation  `json:"put,omitempty"`
	Patch      *Operation  `json:"patch,omitempty"`
	Delete     *Operation  `json:"delete,omitempty"`
}

type Operation struct {
	Tags        []string               `json:"tags,omitempty"`
	Summary     string                 `json:"summary,omitempty"`
	Description string                 `json:"description,omitempty"`
	OperationID string                 `json:"operationId,omitempty"`
	Parameters  []Parameter            `json:"parameters,omitempty"`
	RequestBody *RequestBody           `json:"requestBody,omitempty"`
	Responses   map[string]Response    `json:"responses"`
	Security    *[]SecurityRequirement `json:"security,omitempty"`
}

func (o *Operation) Public() *Operation {
	o.Security = &[]SecurityRequirement{}
	return o
}

type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

type RequestBody struct {
	Required bool                 `json:"required,omitempty"`
	Content  map[string]MediaType `json:"content"`
}

type MediaType struct {
	Schema *Schema `json:"schema"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Description          string             `json:"description,omitempty"`
	ReadOnly             bool               `json:"readOnly,omitempty"`
	MinLength            int                `json:"minLength,omitempty"`
	MaxLength            int                `json:"maxLength,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Examples             []any              `json:"examples,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
}

type Components struct {
	Schemas         map[string]*Schema        `json:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

type SecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	Description  string `json:"description,omitempty"`
}

type SecurityRequirement map[string][]string

func Ref(name string) *Schema {
	return &Schema{Ref: "#/components/schemas/" + name}
}

func String() *Schema { return &Schema{Type: "string"} }

func Integer() *Schema { return &Schema{Type: "integer"} }

func Array(items *Schema) *Schema {
	return &Schema{Type: "array", Items: items}
}

func Object(properties map[string]*Schema, required ...string) *Schema {
	return &Schema{Type: "object", Properties: properties, Required: required}
}

func Dictionary(values *Schema) *Schema {
	return &Schema{Type: "object", AdditionalProperties: values}
}

func Body(schema *Schema) *RequestBody {
	return &RequestBody{
		Required: true,
		Content:  map[string]MediaType{"application/json": {Schema: schema}},
	}
}

func JSON(description string, schema *Schema) Response {
	return Response{
		Description: description,
		Content:     map[string]MediaType{"application/json": {Schema: schema}},
	}
}

func Empty(description string) Response {
	return Response{Description: description}
}

func Failure(description string) Response {
	return JSON(description, Object(map[string]*Schema{
		"error":  String(),
		"status": Integer(),
	}))
}

func PathParameter(name, description string, schema *Schema) Parameter {
	return Parameter{Name: name, In: "path", Required: true, Description: description, Schema: schema}
}

func QueryParameter(name, description string, schema *Schema) Parameter {
	return Parameter{Name: name, In: "query", Description: description, Schema: schema}
}
