package idgen_test

import (
	"testing"

	"github.com/BabySid/aether/idgen"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gaiaidgen "gaia/idgen"
)

func TestGenerateUUIDv4(t *testing.T) {
	generator := gaiaidgen.Generator{}
	contexts := []idgen.Context{
		{WorkflowKind: "Workflow"},
		{WorkflowRunID: "workflow-1", WorkflowKind: "Workflow", TaskName: "send", TemplateName: "email"},
		{WorkflowKind: "CronWorkflow"},
	}

	seen := make(map[string]bool)
	for _, ctx := range contexts {
		id := generator.Generate(ctx)
		parsed, err := uuid.Parse(id)
		require.NoError(t, err, "Generate(%+v) = %q", ctx, id)
		assert.Equal(t, parsed.String(), id, "Generate(%+v) should be canonical", ctx)
		assert.Equal(t, uuid.Version(4), parsed.Version(), "Generate(%+v)", ctx)
		assert.Equal(t, uuid.RFC4122, parsed.Variant(), "Generate(%+v)", ctx)
		assert.NotContains(t, seen, id, "Generate(%+v) should be unique", ctx)
		seen[id] = true
	}

	id := generator.Generate(contexts[0])
	assert.NotContains(t, seen, id, "repeated Generate(%+v) should be unique", contexts[0])
}
