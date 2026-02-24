package types

// GeneratedDoc is the LLM output document.
type GeneratedDoc struct {
	Scenario  string      `json:"scenario"`
	CallChain []ChainStep `json:"call_chain"`
	Endpoints []Endpoint  `json:"endpoints"`
}

// ChainStep is one step in the call chain.
type ChainStep struct {
	Seq         int    `json:"seq"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
	DependsOn   *int   `json:"depends_on,omitempty"`
}

// Endpoint describes one API endpoint.
type Endpoint struct {
	Method      string      `json:"method"`
	Path        string      `json:"path"`
	Summary     string      `json:"summary"`
	Tags        []string    `json:"tags,omitempty"`
	Description string      `json:"description"`
	PathParams  []Param     `json:"path_params,omitempty"`
	QueryParams []Param     `json:"query_params,omitempty"`
	RequestBody *BodySchema `json:"request_body,omitempty"`
	Responses   []Response  `json:"responses"`
	Example     *Example    `json:"example,omitempty"`
}

// Param defines a parameter (supports nested children).
type Param struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Required    bool    `json:"required"`
	Description string  `json:"description"`
	Children    []Param `json:"children,omitempty"`
}

// BodySchema describes a request body.
type BodySchema struct {
	ContentType string  `json:"content_type"`
	Fields      []Param `json:"fields"`
}

// Response defines a response schema.
type Response struct {
	StatusCode  int     `json:"status_code"`
	ContentType string  `json:"content_type,omitempty"`
	Description string  `json:"description"`
	Fields      []Param `json:"fields,omitempty"`
}

// Example provides example request/response.
type Example struct {
	Request  string `json:"request"`
	Response string `json:"response"`
}
