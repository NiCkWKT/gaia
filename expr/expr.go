package expr

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	aetherexpr "github.com/BabySid/aether/expr"
	lang "github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser/lexer"
)

// Evaluator evaluates Aether expressions without external services or shared state.
type Evaluator struct{}

var _ aetherexpr.Evaluator = Evaluator{}

func (Evaluator) Eval(ctx context.Context, expression string, env map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	input, err := quoteHyphenatedKeys(expression, env)
	if err != nil {
		return nil, fmt.Errorf("lex expression: %w", err)
	}
	program, err := lang.Compile(input, lang.Env(env), lang.Patch(flatKeys{env: env}))
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

// quoteHyphenatedKeys protects exact flat keys before expr parses '-' as subtraction.
func quoteHyphenatedKeys(expression string, env map[string]any) (string, error) {
	tokens, err := lexer.Lex(file.NewSource(expression))
	if err != nil {
		return "", err
	}
	source := []rune(expression)
	var result strings.Builder
	last := 0
	for _, token := range tokens {
		if token.Kind != lexer.Identifier || token.From < last || token.From > 0 && source[token.From-1] == '.' {
			continue
		}
		match := longestHyphenatedKey(source, token.From, env)
		if match == "" {
			continue
		}
		result.WriteString(string(source[last:token.From]))
		parts := strings.Split(match, ".")
		result.WriteString(parts[0])
		for _, part := range parts[1:] {
			if strings.Contains(part, "-") {
				result.WriteByte('[')
				result.WriteString(strconv.Quote(part))
				result.WriteByte(']')
			} else {
				result.WriteByte('.')
				result.WriteString(part)
			}
		}
		last = token.From + len([]rune(match))
	}
	result.WriteString(string(source[last:]))
	return result.String(), nil
}

func longestHyphenatedKey(source []rune, start int, env map[string]any) string {
	var match string
	for key := range env {
		if !strings.Contains(key, "-") || len([]rune(key)) <= len([]rune(match)) {
			continue
		}
		end := start + len([]rune(key))
		if end > len(source) || string(source[start:end]) != key {
			continue
		}
		if end < len(source) && (isNameRune(source[end]) || source[end] == '-') {
			continue
		}
		match = key
	}
	return match
}

func isNameRune(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
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
