package brunogen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// parseStructs extracts request/response structs from API files
func (g *Generator) parseStructs() error {
	apiFiles := []string{"jobs.go", "sync.go", "system.go"}

	for _, file := range apiFiles {
		filePath := filepath.Join(g.apiDir, file)
		if err := g.parseStructsFromFile(filePath); err != nil {
			// Continue if file doesn't exist or has issues
			continue
		}
	}

	// Map routes to their request bodies
	g.mapRoutesToStructs()

	return nil
}

// parseStructsFromFile extracts structs from a single file
func (g *Generator) parseStructsFromFile(filePath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", filePath, err)
	}

	// Find all struct declarations
	ast.Inspect(node, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			return true
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			// Only process request structs
			structName := typeSpec.Name.Name
			if !strings.HasSuffix(structName, "Request") {
				continue
			}

			info := g.parseStruct(structName, structType)
			g.structs[structName] = info
		}

		return true
	})

	return nil
}

// parseStruct extracts field information from a struct
func (g *Generator) parseStruct(name string, structType *ast.StructType) StructInfo {
	info := StructInfo{
		Name:   name,
		Fields: []FieldInfo{},
	}

	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			continue
		}

		fieldName := field.Names[0].Name
		typeName := g.typeToString(field.Type)
		jsonTag := g.extractJSONTag(field.Tag)
		required := !strings.Contains(g.getTagString(field.Tag), ",omitempty")
		example := g.getExampleForField(fieldName, typeName)

		info.Fields = append(info.Fields, FieldInfo{
			Name:     fieldName,
			Type:     typeName,
			JSONTag:  jsonTag,
			Required: required,
			Example:  example,
		})
	}

	return info
}

// typeToString converts an AST type expression to a string
func (g *Generator) typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.ArrayType:
		return "[]" + g.typeToString(t.Elt)
	case *ast.MapType:
		return "map[" + g.typeToString(t.Key) + "]" + g.typeToString(t.Value)
	case *ast.SelectorExpr:
		// For types like models.JobMetadata
		return t.Sel.Name
	case *ast.StarExpr:
		return "*" + g.typeToString(t.X)
	default:
		return "interface{}"
	}
}

// extractJSONTag extracts the JSON tag name from a field tag
func (g *Generator) extractJSONTag(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}

	tagStr := g.getTagString(tag)
	jsonTagStart := strings.Index(tagStr, `json:"`)
	if jsonTagStart == -1 {
		return ""
	}

	jsonTagStart += 6 // len(`json:"`)
	jsonTagEnd := strings.Index(tagStr[jsonTagStart:], `"`)
	if jsonTagEnd == -1 {
		return ""
	}

	jsonTag := tagStr[jsonTagStart : jsonTagStart+jsonTagEnd]
	// Remove omitempty and other options
	parts := strings.Split(jsonTag, ",")
	if parts[0] == "-" {
		return ""
	}
	return parts[0]
}

// getTagString returns the string value of a tag
func (g *Generator) getTagString(tag *ast.BasicLit) string {
	if tag == nil {
		return ""
	}
	return strings.Trim(tag.Value, "`")
}

// getExampleForField generates an example value for a field
func (g *Generator) getExampleForField(fieldName, typeName string) interface{} {
	fieldLower := strings.ToLower(fieldName)

	// Special cases based on field name
	switch {
	case strings.Contains(fieldLower, "name"):
		return "Example Download"
	case strings.Contains(fieldLower, "path"):
		if strings.Contains(fieldLower, "remote") {
			return "/downloads/completed/dp/example-file"
		}
		return "/local/path/to/file"
	case strings.Contains(fieldLower, "priority"):
		return 5
	case strings.Contains(fieldLower, "retries"):
		return 3
	case strings.Contains(fieldLower, "size"):
		return 1073741824 // 1GB
	case strings.Contains(fieldLower, "category"):
		return "movies"
	case strings.Contains(fieldLower, "tags"):
		return []string{"example", "tag"}
	case strings.Contains(fieldLower, "hash"):
		return "abc123def456"
	case fieldLower == "metadata":
		// Return as a struct marker to be handled specially in JSON generation
		return "METADATA_OBJECT"
	}

	// Default based on type
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
		if strings.HasPrefix(typeName, "map[") {
			return map[string]interface{}{}
		}
		return nil
	}
}

// mapRoutesToStructs links routes to their request body structs
func (g *Generator) mapRoutesToStructs() {
	for i := range g.routes {
		route := &g.routes[i]

		// Map handler names to struct names
		var structName string
		switch route.Handler {
		case "CreateJob":
			structName = "CreateJobRequest"
		case "CreateSync":
			structName = "CreateSyncRequest"
		default:
			continue
		}

		if info, ok := g.structs[structName]; ok {
			route.RequestBody = &info
		}
	}
}
