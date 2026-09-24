package expr_test

import (
	"context"
	"gaia/expr"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluator(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		env        map[string]any
		want       any
		wantErr    bool
	}{
		{"boolean condition", "tasks.fetch.phase != 'Succeeded'", map[string]any{"tasks.fetch.phase": "Failed"}, true, false},
		{"flat nil value is present", "tasks.fetch.phase == nil", map[string]any{"tasks.fetch.phase": nil}, true, false},
		{"flat key takes precedence", "tasks.fetch.phase", map[string]any{"tasks.fetch.phase": "Succeeded", "tasks": map[string]any{"fetch": map[string]any{"phase": "Failed"}}}, "Succeeded", false},
		{"flat prefix and nested member", "tasks.fetch.phase", map[string]any{"tasks.fetch": map[string]any{"phase": "Running"}}, "Running", false},
		{"nested map", "tasks.fetch.phase", map[string]any{"tasks": map[string]any{"fetch": map[string]any{"phase": "Failed"}}}, "Failed", false},
		{"flat bracket notation", `tasks["fetch"].phase`, map[string]any{"tasks.fetch.phase": "Succeeded"}, "Succeeded", false},
		{"loop index", "loop_iter.index + 1", map[string]any{"loop_iter.index": 2}, 3, false},
		{"workflow parameter", "workflow.parameters.count * 2", map[string]any{"workflow.parameters.count": float64(3)}, float64(6), false},
		{"array result", "[1, 2, 3]", nil, []any{1, 2, 3}, false},
		{"builtin", `len([1, 2])`, nil, 2, false},
		{"missing root", "tasks.fetch.phase", nil, nil, true},
		{"missing flat key", "tasks.fetch.phase", map[string]any{"tasks.fetch.code": 0}, nil, true},
		{"missing nested member", "tasks.fetch.phase", map[string]any{"tasks": map[string]any{"fetch": map[string]any{}}}, nil, true},
		{"missing nested map", "tasks.fetch.phase", map[string]any{"tasks": map[string]any{}}, nil, true},
		{"bad syntax", "1 +", nil, nil, true},
		{"runtime error", `int("not a number")`, nil, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var original map[string]any
			if tt.env != nil {
				original = map[string]any{}
				for k, v := range tt.env {
					original[k] = v
				}
			}
			got, err := (expr.Evaluator{}).Eval(t.Context(), tt.expression, tt.env)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
			assert.Equal(t, original, tt.env)
		})
	}
}

func TestEvaluatorCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := (expr.Evaluator{}).Eval(ctx, "1 + 2", nil)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
}
