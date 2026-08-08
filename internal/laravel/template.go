// Adapter-owned pipeline template of the Laravel adapter (TS-007-038,
// TS-018-01-02).
//
// The build pipeline definition the adapter owns is returned through the
// `template` command (contracts.CommandTemplate) and written by the Core
// to .anvil/pipelines/build.yaml at generation time, replacing the
// Core-embedded template function pipeline.LaravelBuildPipeline (ADR-020
// §1: framework knowledge moves OUT of the Core binary INTO the adapter
// binaries).
//
// The definition is DERIVED from the adapter's build phase table
// (internal/laravel/build.go — the single source of build knowledge):
// every build phase becomes one template task whose command is the
// phase's program and whose arguments are the phase's runner-form
// arguments translated to the full task command line. Deriving the
// template from the phase table guarantees the generated build.yaml
// always covers the framework's build steps and can never drift from
// the steps the adapter executes (TS-018-01-02, Review 19 §3.3). The
// stage/task layout keeps the structure of the pre-ADR-020 Core template
// (stages dependencies / assets / optimize, task names composer-install /
// npm-build / cache-config / cache-route / cache-view) so existing
// projects' build.yaml stays unchanged and the generated YAML passes the
// pipeline loader validation (pipeline.ParsePipeline).
//
// The CI definition mirrors the generic CI scaffold the Core used to own
// (build + test placeholder stages): the CI pipeline is generic
// placeholder data, not framework knowledge, and supplying it here keeps
// the ci.yaml output of framework initializations complete. The Core no
// longer owns default pipeline template data and no longer falls back to
// a Core-owned CI pipeline when the adapter omits the CI definition
// (TS-015-01-02, ADR-026 decision 1) — the adapter supplies the full
// template set.
//
// Reference: TS-007-038, ADR-020 §1, MVP-002 §3.5, TS-018-01-02
package laravel

import (
	"maleolabs.com/anvil-standard-laravel/internal/contracts"
	"maleolabs.com/anvil-standard-laravel/internal/pipeline"
)

// buildTemplateStage maps each build phase to its template stage. The
// mapping preserves the stage layout of the pre-ADR-020 Core template:
// dependencies (composer), assets (npm), optimize (artisan caches).
// Every phase in buildPhases must have an entry — the template tests
// enforce the mapping stays complete when the phase table grows
// (TS-018-01-02).
var buildTemplateStage = map[string]string{
	PhaseComposer:    "dependencies",
	PhaseNpm:         "assets",
	PhaseConfigCache: "optimize",
	PhaseRouteCache:  "optimize",
	PhaseViewCache:   "optimize",
}

// buildTemplateTaskName maps each build phase to its template task name.
// The mapping preserves the task names of the pre-ADR-020 Core template
// so generated build.yaml stays byte-compatible for new projects and
// existing projects' files keep matching task identifiers (target
// selection and environment overrides key on task names). Every phase in
// buildPhases must have an entry — the template tests enforce the
// mapping stays complete when the phase table grows (TS-018-01-02).
var buildTemplateTaskName = map[string]string{
	PhaseComposer:    "composer-install",
	PhaseNpm:         "npm-build",
	PhaseConfigCache: "cache-config",
	PhaseRouteCache:  "cache-route",
	PhaseViewCache:   "cache-view",
}

// Template returns the pipeline definitions the Laravel adapter owns:
// the build pipeline and the CI scaffold. The Core validates them
// through the pipeline loader and writes them to .anvil/pipelines/ at
// generation time (ADR-020 §1).
//
// Reference: TS-007-038, ADR-020 §1
func Template() contracts.TemplateResult {
	return contracts.TemplateResult{
		Build: buildPipelineTemplate(),
		CI:    ciPipelineTemplate(),
	}
}

// buildPipelineTemplate derives the build pipeline definition from the
// build phase table (buildPhases — the single source of build
// knowledge): each phase becomes one task in its mapped stage, in table
// order, with the phase's program as the command and the phase's
// runner-form arguments translated to the full task command line
// (TS-018-01-02). Stages are emitted in first-appearance order —
// dependencies, assets, optimize — matching the pre-ADR-020 template.
func buildPipelineTemplate() *pipeline.PipelineDefinition {
	stages := []pipeline.PipelineStage{}
	stageIndex := map[string]int{}

	for _, phase := range buildPhases {
		stageName := templateStage(phase)
		idx, ok := stageIndex[stageName]
		if !ok {
			stages = append(stages, pipeline.PipelineStage{Name: stageName})
			idx = len(stages) - 1
			stageIndex[stageName] = idx
		}
		stages[idx].Tasks = append(stages[idx].Tasks, templateTask(phase))
	}

	return &pipeline.PipelineDefinition{
		Pipeline: pipeline.Pipeline{
			Name:   "build",
			Stages: stages,
		},
	}
}

// templateStage returns the template stage a build phase belongs to. The
// mapping keeps the pre-ADR-020 stage layout; an unmapped phase falls
// back to the "optimize" stage so the template command stays functional
// during development — the completeness test fails before an unmapped
// phase can ship (TS-018-01-02).
func templateStage(phase buildPhase) string {
	if stage, ok := buildTemplateStage[phase.name]; ok {
		return stage
	}
	return "optimize"
}

// templateTaskName returns the template task name for a build phase. The
// mapping keeps the pre-ADR-020 task names; an unmapped phase falls back
// to its phase name so the emitted task stays named and valid — the
// completeness test fails before an unmapped phase can ship
// (TS-018-01-02).
func templateTaskName(phase buildPhase) string {
	if name, ok := buildTemplateTaskName[phase.name]; ok {
		return name
	}
	return phase.name
}

// templateTask translates one build phase into a pipeline task: the
// phase's program becomes the command, and the phase's runner-form
// arguments become the full task command line. Artisan phases run as
// `php artisan <args>` — the "artisan" prefix mirrors the production
// runner runArtisan (internal/laravel/activation.go), which prepends it
// to the phase's args; composer and npm phases run as `composer <args>`
// and `npm <args>` — their args are already the full argument vector
// (runComposer / runNpm take them as given).
func templateTask(phase buildPhase) pipeline.Task {
	args := phase.args
	if phase.program == "php" {
		args = append([]string{"artisan"}, phase.args...)
	}
	return pipeline.Task{
		Name:    templateTaskName(phase),
		Command: phase.program,
		Args:    args,
	}
}

// ciPipelineTemplate returns the generic CI scaffold the adapter
// supplies: a build stage and a test stage with placeholder tasks. The
// CI pipeline is generic placeholder data — not framework knowledge —
// and is replaced by the project's own CI pipeline; supplying it keeps
// the ci.yaml output of framework initializations complete (ADR-026
// decision 1).
func ciPipelineTemplate() *pipeline.PipelineDefinition {
	return &pipeline.PipelineDefinition{
		Pipeline: pipeline.Pipeline{
			Name: "ci",
			Stages: []pipeline.PipelineStage{
				{
					Name: "build",
					Tasks: []pipeline.Task{
						{
							Name:    "build",
							Command: "echo",
							Args:    []string{"building..."},
						},
					},
				},
				{
					Name: "test",
					Tasks: []pipeline.Task{
						{
							Name:    "unit-tests",
							Command: "echo",
							Args:    []string{"running unit tests..."},
						},
						{
							Name:    "static-analysis",
							Command: "echo",
							Args:    []string{"running static analysis..."},
						},
						{
							Name:    "linting",
							Command: "echo",
							Args:    []string{"running linter..."},
						},
					},
				},
			},
		},
	}
}
