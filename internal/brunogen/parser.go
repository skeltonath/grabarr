package brunogen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// parseRoutes extracts API routes from handlers.go
func (g *Generator) parseRoutes() error {
	handlersFile := filepath.Join(g.apiDir, "handlers.go")

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, handlersFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse handlers.go: %w", err)
	}

	// Find the RegisterRoutes method
	ast.Inspect(node, func(n ast.Node) bool {
		funcDecl, ok := n.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != "RegisterRoutes" {
			return true
		}

		// Traverse function body to find HandleFunc calls
		ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
			callExpr, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}

			// Look for api.HandleFunc(...) calls
			if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "HandleFunc" && len(callExpr.Args) >= 2 {
					g.extractRoute(callExpr)
				}
			}

			// Also look for Methods(...) calls
			if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "Methods" && len(callExpr.Args) > 0 {
					// Get the parent HandleFunc call
					// This is a chained call like api.HandleFunc("/path", handler).Methods("POST")
					if innerCall, ok := sel.X.(*ast.CallExpr); ok {
						if innerSel, ok := innerCall.Fun.(*ast.SelectorExpr); ok {
							if innerSel.Sel.Name == "HandleFunc" {
								g.extractRouteWithMethod(innerCall, callExpr)
							}
						}
					}
				}
			}

			return true
		})

		return false
	})

	return nil
}

// extractRoute extracts route info from a HandleFunc call
func (g *Generator) extractRoute(call *ast.CallExpr) {
	if len(call.Args) < 2 {
		return
	}

	// Extract path (first argument)
	pathLit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || pathLit.Kind != token.STRING {
		return
	}
	path := strings.Trim(pathLit.Value, `"`)

	// Extract handler name (second argument)
	var handlerName string
	switch h := call.Args[1].(type) {
	case *ast.SelectorExpr:
		handlerName = h.Sel.Name
	case *ast.Ident:
		handlerName = h.Name
	}

	// Create route (method will be determined by handler name or later)
	route := Route{
		Path:       path,
		Handler:    handlerName,
		PathParams: extractPathParams(path),
	}

	// Try to determine method from handler name
	route.Method = inferMethodFromHandler(handlerName)

	// Determine category from path
	route.Category = extractCategory(path)

	g.routes = append(g.routes, route)
}

// extractRouteWithMethod extracts route with explicit method
func (g *Generator) extractRouteWithMethod(handleFuncCall *ast.CallExpr, methodsCall *ast.CallExpr) {
	if len(handleFuncCall.Args) < 2 || len(methodsCall.Args) < 1 {
		return
	}

	// Extract path
	pathLit, ok := handleFuncCall.Args[0].(*ast.BasicLit)
	if !ok || pathLit.Kind != token.STRING {
		return
	}
	path := strings.Trim(pathLit.Value, `"`)

	// Extract handler name
	var handlerName string
	switch h := handleFuncCall.Args[1].(type) {
	case *ast.SelectorExpr:
		handlerName = h.Sel.Name
	case *ast.Ident:
		handlerName = h.Name
	}

	// Extract method
	methodLit, ok := methodsCall.Args[0].(*ast.BasicLit)
	if !ok || methodLit.Kind != token.STRING {
		return
	}
	method := strings.Trim(methodLit.Value, `"`)

	route := Route{
		Method:     method,
		Path:       path,
		Handler:    handlerName,
		Category:   extractCategory(path),
		PathParams: extractPathParams(path),
	}

	g.routes = append(g.routes, route)
}

// extractPathParams finds path parameters in a route path
func extractPathParams(path string) []string {
	matches := pathParamRegex.FindAllStringSubmatch(path, -1)
	var params []string
	for _, match := range matches {
		if len(match) > 1 {
			// Extract just the parameter name from patterns like {id:[0-9]+}
			paramName := strings.Split(match[1], ":")[0]
			params = append(params, paramName)
		}
	}
	return params
}

// extractCategory determines the category (folder) from the path
func extractCategory(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		firstPart := parts[0]
		// Handle /jobs, /sync, /health, /metrics, /status
		switch firstPart {
		case "jobs":
			return "jobs"
		case "sync":
			return "sync"
		case "health", "metrics", "status":
			return "system"
		default:
			return "misc"
		}
	}
	return "misc"
}

// inferMethodFromHandler tries to determine HTTP method from handler name
func inferMethodFromHandler(handlerName string) string {
	lower := strings.ToLower(handlerName)
	switch {
	case strings.HasPrefix(lower, "create"):
		return "POST"
	case strings.HasPrefix(lower, "get"):
		return "GET"
	case strings.HasPrefix(lower, "delete"):
		return "DELETE"
	case strings.HasPrefix(lower, "update"):
		return "PUT"
	case strings.HasPrefix(lower, "cancel"):
		return "POST"
	case strings.Contains(lower, "health"):
		return "GET"
	case strings.Contains(lower, "metrics"):
		return "GET"
	case strings.Contains(lower, "status"):
		return "GET"
	default:
		return "GET"
	}
}
