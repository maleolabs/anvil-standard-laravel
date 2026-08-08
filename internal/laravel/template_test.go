// Tests for the Laravel adapter-owned pipeline template (TS-007-038,
// TS-018-01-02): the build pipeline definition derived from the build
// phase table and the generic CI scaffold, and the wire shape the Core
// consumes through the `template` command.
package laravel

import (
	"encoding/json"
	"reflect"
	"testing"

	"maleolabs.com/anvil-standard-laravel/internal/contracts"
	"maleolabs.com/anvil-standard-laravel/internal/pipeline"
)

// TestTemplate_CoversBuildPhases verifies the build template covers the
// framework's build steps: every phase in the build phase table (the
// single source of build knowledge) has exactly one template task whose
// command is the phase's program and whose arguments are the phase's
// full task command line, and every template task corresponds to a
// declared phase. The template can never drift from the executed build
// (TS-018-01-02, Review 19 §3.3).
func TestTemplate_CoversBuildPhases(t *testing.T) {
	tmpl := Template()
	if tmpl.Build == nil {
		t.Fatal("Build = nil, want a build definition")
	}

	// Every build phase must appear exactly once in the template, with
	// the full command line the phase executes.
	tasks := templateTasks(t, tmpl.Build)
	seen := map[string]bool{}
	for _, phase := range buildPhases {
		var matched bool
		for _, task := range tasks {
			if task.Name != templateTaskName(phase) {
				continue
			}
			if matched {
				t.Fatalf("phase %q maps to more than one template task", phase.name)
			}
			matched = true
			if task.Command != phase.program {
				t.Errorf("task %q command = %q, want phase program %q", task.Name, task.Command, phase.program)
			}
			if want := templateTaskArgs(phase); !reflect.DeepEqual(task.Args, want) {
				t.Errorf("task %q args = %v, want %v", task.Name, task.Args, want)
			}
		}
		if !matched {
			t.Errorf("build phase %q (program %q, args %v) has no template task", phase.name, phase.program, phase.args)
		}
		seen[templateTaskName(phase)] = true
	}

	// Every template task must come from a declared build phase — an
	// orphan task would run a step the build phase table does not own.
	for _, task := range tasks {
		if !seen[task.Name] {
			t.Errorf("template task %q does not correspond to any build phase", task.Name)
		}
	}
}

// TestTemplate_StageTaskLayout verifies the stage/task layout matches the
// pre-ADR-020 Core template exactly: stages dependencies → assets →
// optimize, with the fixed task names and full command lines — so
// existing projects' build.yaml stays unchanged and generated YAML keeps
// passing the pipeline loader validation (TS-007-038, TS-018-01-02).
func TestTemplate_StageTaskLayout(t *testing.T) {
	tmpl := Template()
	p := tmpl.Build.Pipeline

	if p.Name != "build" {
		t.Errorf("pipeline name = %q, want \"build\"", p.Name)
	}

	wantStages := []struct {
		name  string
		tasks []pipeline.Task
	}{
		{
			name: "dependencies",
			tasks: []pipeline.Task{
				{
					Name:    "composer-install",
					Command: "composer",
					Args:    []string{"install", "--no-dev", "--optimize-autoloader"},
				},
			},
		},
		{
			name: "assets",
			tasks: []pipeline.Task{
				{
					Name:    "npm-build",
					Command: "npm",
					Args:    []string{"run", "build"},
				},
			},
		},
		{
			name: "optimize",
			tasks: []pipeline.Task{
				{
					Name:    "cache-config",
					Command: "php",
					Args:    []string{"artisan", "config:cache"},
				},
				{
					Name:    "cache-route",
					Command: "php",
					Args:    []string{"artisan", "route:cache"},
				},
				{
					Name:    "cache-view",
					Command: "php",
					Args:    []string{"artisan", "view:cache"},
				},
			},
		},
	}

	if len(p.Stages) != len(wantStages) {
		t.Fatalf("stage count = %d, want %d", len(p.Stages), len(wantStages))
	}
	for i, want := range wantStages {
		stage := p.Stages[i]
		if stage.Name != want.name {
			t.Errorf("stage %d name = %q, want %q", i, stage.Name, want.name)
		}
		if len(stage.Tasks) != len(want.tasks) {
			t.Errorf("stage %q task count = %d, want %d", stage.Name, len(stage.Tasks), len(want.tasks))
			continue
		}
		for j, wantTask := range want.tasks {
			task := stage.Tasks[j]
			if task.Name != wantTask.Name {
				t.Errorf("stage %q task %d name = %q, want %q", stage.Name, j, task.Name, wantTask.Name)
			}
			if task.Command != wantTask.Command {
				t.Errorf("stage %q task %q command = %q, want %q", stage.Name, task.Name, task.Command, wantTask.Command)
			}
			if !reflect.DeepEqual(task.Args, wantTask.Args) {
				t.Errorf("stage %q task %q args = %v, want %v", stage.Name, task.Name, task.Args, wantTask.Args)
			}
		}
	}
}

// TestTemplate_StageTaskMappingComplete verifies the layout mappings
// cover every build phase: adding a phase to the build table without a
// deliberate stage/task-name decision fails this test — the layout is a
// compatibility surface (pre-ADR-020 task names key target selection and
// environment overrides), so it must never be extended implicitly
// (TS-018-01-02).
func TestTemplate_StageTaskMappingComplete(t *testing.T) {
	for _, phase := range buildPhases {
		if _, ok := buildTemplateStage[phase.name]; !ok {
			t.Errorf("phase %q has no template stage mapping; add a deliberate stage decision to buildTemplateStage", phase.name)
		}
		if _, ok := buildTemplateTaskName[phase.name]; !ok {
			t.Errorf("phase %q has no template task-name mapping; add a deliberate task name to buildTemplateTaskName", phase.name)
		}
	}
}

// TestTemplate_DefinitionValid verifies the build and CI definitions
// satisfy the structural rules of the runtime's pipeline loader
// validation (execution.PipelineDefinition.Validate, mirrored in
// internal/pipeline): non-empty pipeline name, at least one stage,
// non-empty stage names, at least one task per stage, and non-empty task
// names and commands. The Core rejects definitions that fail this
// validation before writing them to .anvil/pipelines/ (ADR-020 §1).
func TestTemplate_DefinitionValid(t *testing.T) {
	tmpl := Template()
	for name, def := range map[string]*pipeline.PipelineDefinition{
		"build": tmpl.Build,
		"ci":    tmpl.CI,
	} {
		if def == nil {
			t.Fatalf("%s definition = nil, want a definition", name)
		}
		p := def.Pipeline
		if p.Name == "" {
			t.Errorf("%s pipeline name = empty, want a name", name)
		}
		if len(p.Stages) == 0 {
			t.Fatalf("%s pipeline has no stages", name)
		}
		for _, stage := range p.Stages {
			if stage.Name == "" {
				t.Errorf("%s pipeline stage has empty name", name)
			}
			if len(stage.Tasks) == 0 {
				t.Errorf("%s pipeline stage %q has no tasks", name, stage.Name)
			}
			for _, task := range stage.Tasks {
				if task.Name == "" {
					t.Errorf("%s pipeline stage %q has a task with empty name", name, stage.Name)
				}
				if task.Command == "" {
					t.Errorf("%s pipeline stage %q task %q has empty command", name, stage.Name, task.Name)
				}
			}
		}
	}
}

// TestTemplate_CI_Scaffold verifies the CI definition carries the
// generic scaffold (build + test placeholder stages) that keeps the
// ci.yaml output of framework initializations complete (ADR-026 decision
// 1).
func TestTemplate_CI_Scaffold(t *testing.T) {
	tmpl := Template()
	if tmpl.CI == nil {
		t.Fatal("CI = nil, want the CI scaffold")
	}
	p := tmpl.CI.Pipeline
	if p.Name != "ci" {
		t.Errorf("CI pipeline name = %q, want \"ci\"", p.Name)
	}
	if len(p.Stages) != 2 {
		t.Fatalf("CI stage count = %d, want 2 (build, test)", len(p.Stages))
	}
	if p.Stages[0].Name != "build" || len(p.Stages[0].Tasks) != 1 {
		t.Errorf("CI first stage = %q with %d tasks, want \"build\" with 1 task", p.Stages[0].Name, len(p.Stages[0].Tasks))
	}
	if p.Stages[1].Name != "test" || len(p.Stages[1].Tasks) != 3 {
		t.Errorf("CI second stage = %q with %d tasks, want \"test\" with 3 tasks", p.Stages[1].Name, len(p.Stages[1].Tasks))
	}
	for _, stage := range p.Stages {
		for _, task := range stage.Tasks {
			if task.Command != "echo" {
				t.Errorf("CI task %q command = %q, want placeholder \"echo\"", task.Name, task.Command)
			}
		}
	}
}

// TestTemplate_WireShape verifies the template command result carries
// both definitions under the "build" and "ci" keys of the JSON payload
// the Core parses (contracts.TemplateResult), with the task shape the
// pipeline loader expects (name/command/args).
func TestTemplate_WireShape(t *testing.T) {
	tmpl := Template()
	if !reflect.DeepEqual(tmpl, contracts.TemplateResult{Build: tmpl.Build, CI: tmpl.CI}) {
		t.Error("Template() result does not carry Build and CI as a contracts.TemplateResult")
	}

	data, err := json.Marshal(tmpl)
	if err != nil {
		t.Fatalf("marshaling template result: %v", err)
	}
	keys := jsonKeys(t, data)
	for _, want := range []string{"build", "ci"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("TemplateResult JSON missing key %q (got %v)", want, keys)
		}
	}

	// A task in the emitted JSON must carry Name, Command and Args —
	// the struct fields the pipeline loader parses. The pipeline
	// mirror carries yaml tags only, so the subprocess JSON uses the
	// Go field names on both sides of the contract (byte-compatible
	// wire shape, ADR-025 §3.4).
	var parsed struct {
		Build struct {
			Pipeline struct {
				Stages []struct {
					Tasks []map[string]any `json:"Tasks"`
				} `json:"Stages"`
			} `json:"Pipeline"`
		} `json:"build"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshaling template result: %v", err)
	}
	tasks := 0
	for _, stage := range parsed.Build.Pipeline.Stages {
		tasks += len(stage.Tasks)
		for _, task := range stage.Tasks {
			if task["Name"] == nil || task["Command"] == nil {
				t.Errorf("task JSON missing Name/Command: %v", task)
			}
		}
	}
	if tasks != len(buildPhases) {
		t.Errorf("template task count = %d, want %d (one per build phase)", tasks, len(buildPhases))
	}
}

// templateTasks flattens the build definition's tasks into one slice.
func templateTasks(t *testing.T, def *pipeline.PipelineDefinition) []pipeline.Task {
	t.Helper()
	var tasks []pipeline.Task
	for _, stage := range def.Pipeline.Stages {
		tasks = append(tasks, stage.Tasks...)
	}
	return tasks
}

// templateTaskArgs is the full task command line a build phase maps to
// (the mirror of templateTask's translation, used by the coverage test
// to state the expected shape explicitly).
func templateTaskArgs(phase buildPhase) []string {
	args := phase.args
	if phase.program == "php" {
		args = append([]string{"artisan"}, phase.args...)
	}
	return args
}

// jsonKeys returns the top-level keys of a JSON document (the pattern of
// the contracts package tests).
func jsonKeys(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}
	keys := make(map[string]bool, len(m))
	for k := range m {
		keys[k] = true
	}
	return keys
}

// TestTemplate_UnknownPhaseFallback guards against a future phase added
// to the table without a mapping: the template command must stay
// functional (deterministic fallbacks, never a panic), and the
// completeness test above flags the missing mapping so the fallback can
// never ship silently (TS-018-01-02).
func TestTemplate_UnknownPhaseFallback(t *testing.T) {
	unknown := buildPhase{name: "future-phase", program: "php", args: []string{"some:command"}}
	if got := templateStage(unknown); got != "optimize" {
		t.Errorf("templateStage(future-phase) = %q, want fallback \"optimize\"", got)
	}
	if got := templateTaskName(unknown); got != "future-phase" {
		t.Errorf("templateTaskName(future-phase) = %q, want fallback phase name", got)
	}
	task := templateTask(unknown)
	if task.Name != "future-phase" || task.Command != "php" {
		t.Errorf("templateTask(future-phase) = %+v, want name/command fallbacks", task)
	}
	if !reflect.DeepEqual(task.Args, []string{"artisan", "some:command"}) {
		t.Errorf("templateTask(future-phase) args = %v, want [artisan some:command]", task.Args)
	}
}
