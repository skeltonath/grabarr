package brunogen

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var pathParamRegex = regexp.MustCompile(`\{([^}]+)\}`)
var brunoVarRegex = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// Generator handles Bruno collection generation
type Generator struct {
	outputDir string
	baseURL   string
	apiDir    string
	routes    []Route
	structs   map[string]StructInfo
}

// Route represents an API endpoint
type Route struct {
	Method      string
	Path        string
	Handler     string
	Category    string
	PathParams  []string
	QueryParams []string
	RequestBody *StructInfo
}

// StructInfo represents a Go struct for request/response
type StructInfo struct {
	Name   string
	Fields []FieldInfo
}

// FieldInfo represents a field in a struct
type FieldInfo struct {
	Name     string
	Type     string
	JSONTag  string
	Required bool
	Example  interface{}
}

// NewGenerator creates a new Bruno collection generator
func NewGenerator(outputDir, baseURL, apiDir string) *Generator {
	return &Generator{
		outputDir: outputDir,
		baseURL:   baseURL,
		apiDir:    apiDir,
		structs:   make(map[string]StructInfo),
	}
}

// Generate creates the Bruno collection
func (g *Generator) Generate() error {
	// Parse API routes from handlers.go
	if err := g.parseRoutes(); err != nil {
		return fmt.Errorf("failed to parse routes: %w", err)
	}

	// Parse request/response structs
	if err := g.parseStructs(); err != nil {
		return fmt.Errorf("failed to parse structs: %w", err)
	}

	// Generate collection metadata files
	if err := g.generateCollectionFiles(); err != nil {
		return fmt.Errorf("failed to generate collection files: %w", err)
	}

	// Generate folders and endpoint files
	if err := g.generateEndpoints(); err != nil {
		return fmt.Errorf("failed to generate endpoints: %w", err)
	}

	return nil
}

// generateCollectionFiles creates bruno.json and collection.bru
func (g *Generator) generateCollectionFiles() error {
	// Generate bruno.json
	brunoJSON := `{
  "version": "1",
  "name": "grabarr",
  "type": "collection",
  "ignore": [
    "node_modules",
    ".git"
  ]
}
`
	if err := os.WriteFile(filepath.Join(g.outputDir, "bruno.json"), []byte(brunoJSON), 0644); err != nil {
		return err
	}

	// Generate collection.bru with common headers/auth
	collectionBru := `headers {
  Content-Type: application/json
}

auth {
  mode: none
}
`
	return os.WriteFile(filepath.Join(g.outputDir, "collection.bru"), []byte(collectionBru), 0644)
}

// generateEndpoints creates folders and .bru files for each endpoint
func (g *Generator) generateEndpoints() error {
	// Group routes by category
	categories := make(map[string][]Route)
	for _, route := range g.routes {
		categories[route.Category] = append(categories[route.Category], route)
	}

	// Generate folders and files for each category
	for category, routes := range categories {
		categoryDir := filepath.Join(g.outputDir, category)
		if err := os.MkdirAll(categoryDir, 0755); err != nil {
			return err
		}

		// Create folder.bru
		folderBru := fmt.Sprintf(`meta {
  name: %s
  seq: 1
}

auth {
  mode: inherit
}
`, category)
		if err := os.WriteFile(filepath.Join(categoryDir, "folder.bru"), []byte(folderBru), 0644); err != nil {
			return err
		}

		// Generate .bru files for each route
		for i, route := range routes {
			if err := g.generateRouteBruFile(categoryDir, route, i+1); err != nil {
				return err
			}
		}
	}

	return nil
}

// generateRouteBruFile creates a .bru file for a single route
func (g *Generator) generateRouteBruFile(dir string, route Route, seq int) error {
	name := g.routeToName(route)
	filename := name + ".bru"

	var content strings.Builder

	// Meta block
	content.WriteString(fmt.Sprintf(`meta {
  name: %s
  type: http
  seq: %d
}

`, name, seq))

	// HTTP method block
	method := strings.ToLower(route.Method)
	url := g.baseURL + "/api/v1" + route.Path

	// Protect Bruno variables ({{baseUrl}}) from being replaced
	brunoVars := []string{}
	url = brunoVarRegex.ReplaceAllStringFunc(url, func(match string) string {
		placeholder := fmt.Sprintf("__BRUNOVAR%d__", len(brunoVars))
		brunoVars = append(brunoVars, match)
		return placeholder
	})

	// Replace path parameters with :param syntax (handle {id} and {id:[0-9]+} patterns)
	url = pathParamRegex.ReplaceAllStringFunc(url, func(match string) string {
		// Extract param name from {name} or {name:pattern}
		paramName := strings.Split(strings.Trim(match, "{}"), ":")[0]
		return ":" + paramName
	})

	// Restore Bruno variables
	for i, brunoVar := range brunoVars {
		placeholder := fmt.Sprintf("__BRUNOVAR%d__", i)
		url = strings.Replace(url, placeholder, brunoVar, 1)
	}

	bodyType := "none"
	if route.RequestBody != nil {
		bodyType = "json"
	}

	content.WriteString(fmt.Sprintf(`%s {
  url: %s
  body: %s
  auth: inherit
}

`, method, url, bodyType))

	// Path params block
	if len(route.PathParams) > 0 {
		content.WriteString("params:path {\n")
		for _, param := range route.PathParams {
			content.WriteString(fmt.Sprintf("  %s: 1\n", param))
		}
		content.WriteString("}\n\n")
	}

	// Query params block (if applicable)
	if len(route.QueryParams) > 0 {
		content.WriteString("params:query {\n")
		for _, param := range route.QueryParams {
			content.WriteString(fmt.Sprintf("  ~%s: \n", param))
		}
		content.WriteString("}\n\n")
	}

	// Body block
	if route.RequestBody != nil {
		content.WriteString("body:json {\n")
		jsonBody := g.generateJSONExample(route.RequestBody)
		content.WriteString(g.indent(jsonBody, 2))
		content.WriteString("\n}\n\n")
	}

	// Settings block
	content.WriteString(`settings {
  encodeUrl: true
}
`)

	return os.WriteFile(filepath.Join(dir, filename), []byte(content.String()), 0644)
}

// routeToName converts a route to a readable name
func (g *Generator) routeToName(route Route) string {
	// Extract the handler function name or create from path
	if route.Handler != "" {
		return route.Handler
	}

	// Fallback: create from method + path
	parts := strings.Split(strings.Trim(route.Path, "/"), "/")
	if len(parts) > 0 {
		return strings.Title(parts[len(parts)-1])
	}

	return route.Method
}

// generateJSONExample creates a JSON example from a struct
func (g *Generator) generateJSONExample(info *StructInfo) string {
	var lines []string
	lines = append(lines, "{")

	for i, field := range info.Fields {
		if field.JSONTag == "" || field.JSONTag == "-" {
			continue
		}

		example := field.Example
		if example == nil {
			example = g.getDefaultExample(field.Type)
		}

		line := fmt.Sprintf(`  "%s": %s`, field.JSONTag, g.formatJSONValue(example))
		if i < len(info.Fields)-1 {
			line += ","
		}
		lines = append(lines, line)
	}

	lines = append(lines, "}")
	return strings.Join(lines, "\n")
}

// formatJSONValue formats a value for JSON output
func (g *Generator) formatJSONValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		// Special handling for metadata object marker
		if v == "METADATA_OBJECT" {
			return `{
      "category": "movies"
    }`
		}
		return fmt.Sprintf(`"%s"`, v)
	case int, int64, float64:
		return fmt.Sprintf("%v", v)
	case bool:
		return fmt.Sprintf("%v", v)
	case []string:
		if len(v) == 0 {
			return "[]"
		}
		quoted := make([]string, len(v))
		for i, s := range v {
			quoted[i] = fmt.Sprintf(`"%s"`, s)
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	default:
		return `""`
	}
}

// getDefaultExample returns a default example value based on type
func (g *Generator) getDefaultExample(typeName string) interface{} {
	switch typeName {
	case "string":
		return ""
	case "int", "int64":
		return 0
	case "float64":
		return 0.0
	case "bool":
		return false
	case "[]string":
		return []string{}
	default:
		return ""
	}
}

// indent adds indentation to each line
func (g *Generator) indent(text string, spaces int) string {
	lines := strings.Split(text, "\n")
	prefix := strings.Repeat(" ", spaces)
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}
