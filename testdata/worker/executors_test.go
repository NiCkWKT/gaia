package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/BabySid/aether/executor"
	"github.com/BabySid/aether/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inputs(prompt string, images []string) *model.Inputs {
	p, err := json.Marshal(prompt)
	if err != nil {
		panic(fmt.Sprintf("marshal prompt: %v", err))
	}
	i, err := json.Marshal(images)
	if err != nil {
		panic(fmt.Sprintf("marshal images: %v", err))
	}
	return &model.Inputs{Parameters: []model.Parameter{
		{Name: "prompt", Value: p}, {Name: "images", Value: i},
	}}
}

func TestExecutors(t *testing.T) {
	image := imageExecutor{}
	prompt := promptExecutor{}
	assert.Equal(t, "image", image.Schema().Type)
	assert.Equal(t, "prompt", prompt.Schema().Type)
	for _, schema := range []model.ExecutorSchema{image.Schema(), prompt.Schema()} {
		require.NotNil(t, schema.Inputs)
		assert.Equal(t, []string{"prompt", "images"}, []string{schema.Inputs.Parameters[0].Name, schema.Inputs.Parameters[1].Name})
		require.NotNil(t, schema.Outputs)
	}
	req := &executor.ExecuteRequest{Inputs: inputs("a cat", []string{"first", "second"})}
	result, err := prompt.Execute(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, result.Parameters, 1)
	assert.Equal(t, `"mock response: a cat"`, string(result.Parameters[0].Value))

	first, err := image.Execute(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, first.Parameters, 1)
	assert.Equal(t, "uri", first.Parameters[0].Name)
	var imageURI string
	require.NoError(t, json.Unmarshal(first.Parameters[0].Value, &imageURI))
	parsed, err := url.Parse(imageURI)
	require.NoError(t, err)
	assert.Equal(t, "file", parsed.Scheme)
	data, err := os.ReadFile(parsed.Path)
	require.NoError(t, err)
	assert.Equal(t, imageJPEG, data)

	again, err := image.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, first.Parameters[0].Value, again.Parameters[0].Value)
	changed, err := image.Execute(t.Context(), &executor.ExecuteRequest{Inputs: inputs("a cat", []string{"second", "first"})})
	require.NoError(t, err)
	assert.NotEqual(t, first.Parameters[0].Value, changed.Parameters[0].Value)
}

func TestInvalidExecutorInputs(t *testing.T) {
	cases := []struct {
		name   string
		inputs *model.Inputs
	}{
		{"nil", nil},
		{"missing images", &model.Inputs{Parameters: []model.Parameter{{Name: "prompt", Value: json.RawMessage(`"hi"`)}}}},
		{"null images", &model.Inputs{Parameters: []model.Parameter{{Name: "prompt", Value: json.RawMessage(`"hi"`)}, {Name: "images", Value: json.RawMessage(`null`)}}}},
		{"wrong prompt", &model.Inputs{Parameters: []model.Parameter{{Name: "prompt", Value: json.RawMessage(`42`)}, {Name: "images", Value: json.RawMessage(`[]`)}}}},
		{"wrong images", &model.Inputs{Parameters: []model.Parameter{{Name: "prompt", Value: json.RawMessage(`"hi"`)}, {Name: "images", Value: json.RawMessage(`[3]`)}}}},
		{"duplicate prompt", &model.Inputs{Parameters: []model.Parameter{{Name: "prompt", Value: json.RawMessage(`"hi"`)}, {Name: "prompt", Value: json.RawMessage(`"bye"`)}, {Name: "images", Value: json.RawMessage(`[]`)}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, plugin := range []executor.Plugin{imageExecutor{}, promptExecutor{}} {
				_, err := plugin.Execute(t.Context(), &executor.ExecuteRequest{Inputs: tc.inputs})
				assert.Error(t, err)
			}
		})
	}
}

func TestCancelledExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, plugin := range []executor.Plugin{imageExecutor{}, promptExecutor{}} {
		_, err := plugin.Execute(ctx, &executor.ExecuteRequest{Inputs: inputs("hi", []string{})})
		assert.ErrorIs(t, err, context.Canceled)
	}
}
