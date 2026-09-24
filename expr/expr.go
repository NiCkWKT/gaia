package expr

import (
	"context"
	"fmt"
	"strings"

	aetherexpr "github.com/BabySid/aether/expr"
	lang "github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
)

// Evaluator evaluates Aether expressions without external services or shared state.
type Evaluator struct{}

var _ aetherexpr.Evaluator = Evaluator{}

func (Evaluator) Eval(ctx context.Context, expression string, env map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	program, err := lang.Compile(expression, lang.Env(env), lang.Patch(flatKeys{env: env}))
	if err != nil {
		return nil, fmt.Errorf("compile expression: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := lang.Run(program, env)
	if err != nil {
		return nil, fmt.Errorf("evaluate expression: %w", err)
	}
	return result, nil
}

type flatKeys struct {
	env map[string]any
}

func (p flatKeys) Visit(node *ast.Node) {
	key, ok := staticPath(*node)
	if !ok {
		return
	}
	if _, ok := p.env[key]; ok {
		ast.Patch(node, &ast.IdentifierNode{Value: key})
		return
	}
	for name := range p.env {
		if strings.HasPrefix(name, key+".") {
			return
		}
	}

	// Expr returns nil for absent map members; reject static missing paths instead.
	if member, ok := (*node).(*ast.MemberNode); ok && !member.Optional && !member.Method {
		parts := strings.Split(key, ".")
		var value any
		var exists bool
		start := 0
		for i := len(parts); i > 0; i-- {
			value, exists = p.env[strings.Join(parts[:i], ".")]
			if exists {
				start = i
				break
			}
		}
		for _, part := range parts[start:] {
			object, ok := value.(map[string]any)
			if !ok || !exists {
				break
			}
			value, exists = object[part]
		}
		if !exists {
			ast.Patch(node, &ast.IdentifierNode{Value: key})
		}
	}
}

func staticPath(node ast.Node) (string, bool) {
	switch n := node.(type) {
	case *ast.IdentifierNode:
		return n.Value, true
	case *ast.MemberNode:
		if n.Method || n.Optional {
			return "", false
		}
		parent, ok := staticPath(n.Node)
		if !ok {
			return "", false
		}
		property, ok := n.Property.(*ast.StringNode)
		if !ok {
			return "", false
		}
		return parent + "." + property.Value, true
	default:
		return "", false
	}
}
