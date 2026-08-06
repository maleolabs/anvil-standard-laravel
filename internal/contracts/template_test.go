// Tests for the template command contract payloads (TS-007-038).
package contracts

import (
	"errors"
	"fmt"
	"testing"

	"maleolabs.com/anvil-standard-laravel/internal/pipeline"
)

// testTemplateDefinition builds a minimal valid pipeline definition for
// the template round-trip tests.
func testTemplateDefinition(name string) *pipeline.PipelineDefinition {
	return &pipeline.PipelineDefinition{
		Pipeline: pipeline.Pipeline{
			Name: name,
			Stages: []pipeline.PipelineStage{
				{
					Name: "build",
					Tasks: []pipeline.Task{
						{
							Name:    "compile",
							Command: "echo",
							Args:    []string{"compiling..."},
						},
					},
				},
			},
		},
	}
}

// TestTemplateRequest_RoundTrip verifies that TemplateRequest survives a
// JSON round-trip.
//
// Reference: TS-007-038
func TestTemplateRequest_RoundTrip(t *testing.T) {
	roundTrip(t, TemplateRequest{Framework: "laravel"})
	roundTrip(t, TemplateRequest{})
}

// TestTemplateResult_RoundTrip verifies that TemplateResult survives a
// JSON round-trip for the build-only, build+ci, and empty variants.
//
// Reference: TS-007-038, ADR-020 §1
func TestTemplateResult_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   TemplateResult
	}{
		{
			name: "build_and_ci",
			in: TemplateResult{
				Build: testTemplateDefinition("build"),
				CI:    testTemplateDefinition("ci"),
			},
		},
		{
			name: "build_only",
			in: TemplateResult{
				Build: testTemplateDefinition("build"),
			},
		},
		{
			name: "empty",
			in:   TemplateResult{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.in)
		})
	}
}

// TestTemplateResult_JSONFieldNames verifies TemplateResult serializes to
// the expected snake_case JSON field names, with nil definitions omitted
// (omitempty) — the Core falls back to its defaults for omitted
// definitions (ADR-020 §1).
//
// Reference: TS-007-038, ADR-020 §1
func TestTemplateResult_JSONFieldNames(t *testing.T) {
	in := TemplateResult{Build: testTemplateDefinition("build")}

	data := roundTrip(t, in)
	m := jsonKeys(t, data)
	if _, ok := m["build"]; !ok {
		t.Errorf("TemplateResult JSON missing key %q (got %v)", "build", m)
	}
	if _, ok := m["ci"]; ok {
		t.Errorf("nil CI definition must be omitted (got %v)", m)
	}
}

// TestTemplateResult_DefinitionsValid verifies that the definitions
// carried by TemplateResult pass the pipeline loader validation — the
// Core validates adapter output through pipeline.ParsePipeline before
// writing it to .anvil/pipelines/ (ADR-020 §1).
//
// Reference: TS-007-038, ADR-020 §1
func TestTemplateResult_DefinitionsValid(t *testing.T) {
	result := TemplateResult{
		Build: testTemplateDefinition("build"),
		CI:    testTemplateDefinition("ci"),
	}
	for name, def := range map[string]*pipeline.PipelineDefinition{
		"build": result.Build,
		"ci":    result.CI,
	} {
		if def == nil {
			t.Fatalf("%s definition = nil, want a definition", name)
		}
		if err := validateTemplateDef(def); err != nil {
			t.Errorf("%s definition failed pipeline validation: %v", name, err)
		}
	}
}

// validateTemplateDef is the standard-side mirror of the Core pipeline
// loader's required-field validation (PipelineDefinition.Validate — the
// standard carries no pipeline engine): pipeline name, at least one
// stage, stage names, at least one task per stage, task name + command.
func validateTemplateDef(def *pipeline.PipelineDefinition) error {
	if def == nil {
		return errNilDefinition
	}
	if def.Pipeline.Name == "" {
		return errMissingName
	}
	if len(def.Pipeline.Stages) == 0 {
		return errors.New("pipeline must have at least one stage")
	}
	for i, stage := range def.Pipeline.Stages {
		if stage.Name == "" {
			return fmt.Errorf("stage %d: name is required", i)
		}
		if len(stage.Tasks) == 0 {
			return fmt.Errorf("stage %q: must have at least one task", stage.Name)
		}
		for j, task := range stage.Tasks {
			if task.Name == "" {
				return fmt.Errorf("stage %q task %d: name is required", stage.Name, j)
			}
			if task.Command == "" {
				return fmt.Errorf("stage %q task %q: command is required", stage.Name, task.Name)
			}
		}
	}
	return nil
}

var (
	errNilDefinition = errors.New("definition is nil")
	errMissingName   = errors.New("pipeline name is required")
)
