package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/BabySid/aether/executor"
	"github.com/BabySid/aether/model"
)

//go:embed testdata/gaia_worker_img1.jpeg
var imageJPEG []byte

type mockInput struct {
	Prompt string   `json:"prompt"`
	Images []string `json:"images"`
}

func bindMockInput(inputs *model.Inputs) (mockInput, error) {
	var input mockInput
	if inputs == nil {
		return input, fmt.Errorf("missing inputs")
	}
	found := make(map[string]bool)
	for _, p := range inputs.Parameters {
		switch p.Name {
		case "prompt", "images":
			if found[p.Name] || len(p.Value) == 0 || string(p.Value) == "null" {
				return input, fmt.Errorf("invalid %s parameter", p.Name)
			}
			found[p.Name] = true
		}
	}
	if !found["prompt"] || !found["images"] {
		return input, fmt.Errorf("prompt and images parameters are required")
	}
	if err := executor.BindInputs(inputs, &input); err != nil {
		return input, err
	}
	if input.Images == nil {
		return input, fmt.Errorf("images must be a list of strings")
	}
	return input, nil
}

type imageExecutor struct{}

type imageOutput struct {
	URI string `json:"uri"`
}

var _ executor.Plugin = imageExecutor{}

func (imageExecutor) Type() string { return "image" }
func (imageExecutor) Schema() model.ExecutorSchema {
	return executor.SchemaOf[mockInput, imageOutput]("image", "1.0", "Returns a local mock JPEG")
}

func (imageExecutor) Execute(ctx context.Context, req *executor.ExecuteRequest) (output *model.ExecOutputs, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("missing execution request")
	}
	input, err := bindMockInput(req.Inputs)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	dir := filepath.Join(os.TempDir(), "gaia-mock-worker-images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create image directory: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%x.jpeg", digest[:16]))
	// Rename keeps readers from observing a partially written image when tasks overlap.
	file, err := os.CreateTemp(dir, ".image-*")
	if err != nil {
		return nil, fmt.Errorf("create image: %w", err)
	}
	defer func() {
		if removeErr := os.Remove(file.Name()); removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, fmt.Errorf("remove temporary image: %w", removeErr))
		}
	}()
	if _, err := file.Write(imageJPEG); err != nil {
		return nil, fmt.Errorf("write image: %w; close: %v", err, file.Close())
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return nil, fmt.Errorf("publish image: %w", err)
	}
	return executor.OutputFrom(imageOutput{URI: (&url.URL{Scheme: "file", Path: path}).String()})
}

type promptExecutor struct{}

type promptOutput struct {
	Text string `json:"text"`
}

var _ executor.Plugin = promptExecutor{}

func (promptExecutor) Type() string { return "prompt" }
func (promptExecutor) Schema() model.ExecutorSchema {
	return executor.SchemaOf[mockInput, promptOutput]("prompt", "1.0", "Returns a deterministic mock response")
}

func (promptExecutor) Execute(ctx context.Context, req *executor.ExecuteRequest) (*model.ExecOutputs, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("missing execution request")
	}
	input, err := bindMockInput(req.Inputs)
	if err != nil {
		return nil, err
	}
	return executor.OutputFrom(promptOutput{Text: "mock response: " + input.Prompt})
}
