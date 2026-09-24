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
		{"hyphenated task output", "tasks.detect-os.outputs.parameters.os == 'darwin'", map[string]any{"tasks.detect-os.outputs.parameters.os": "darwin"}, true, false},
		{"hyphenated key inside string remains a string", `"tasks.detect-os"`, map[string]any{"tasks.detect-os": "darwin"}, "tasks.detect-os", false},
		{"hyphenated name after non-ASCII text", `"日本語" == "日本語" && tasks.detect-os.outputs.parameters.os == 'darwin'`, map[string]any{"tasks.detect-os.outputs.parameters.os": "darwin"}, true, false},
		{"hyphen in string is not a key", `"tasks.detect-os.outputs.parameters.os"`, map[string]any{"tasks.detect-os.outputs.parameters.os": "darwin"}, "tasks.detect-os.outputs.parameters.os", false},
		{"hyphenated task output with subtraction", "tasks.detect-os.outputs.parameters.count - 1", map[string]any{"tasks.detect-os.outputs.parameters.count": 3}, 2, false},
		{"hyphenated task output navigates object", "tasks.detect-os.outputs.parameters.metadata.version", map[string]any{"tasks.detect-os.outputs.parameters.metadata": map[string]any{"version": "v1"}}, "v1", false},
		{"hyphenated task with missing output", "tasks.detect-os.outputs.parameters.os", map[string]any{"tasks.other.phase": "Succeeded"}, nil, true},
		{"subtraction remains subtraction", "tasks.detect - os", map[string]any{"tasks.detect": 8, "os": 3}, 5, false},
		{"nested subtraction is not a flat key", "foo.tasks.detect - os", map[string]any{"foo": map[string]any{"tasks": map[string]any{"detect": 8}}, "tasks.detect-os": 99, "os": 3}, 5, false},
		{"hyphenated flat key does not hijack nested path", "foo.tasks.detect-os", map[string]any{"foo": map[string]any{"tasks": map[string]any{"detect": 8}}, "tasks.detect-os": 99, "os": 3}, 5, false},
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
