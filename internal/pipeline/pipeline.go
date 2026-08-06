// Package pipeline mirrors the Core's pipeline definition types
// (maleolabs.com/anvil/internal/execution, pipeline.go) for the template
// command (contracts.TemplateResult): the standard returns its build and
// CI pipeline definitions through the `template` command as JSON, and the
// Core validates and writes them through its pipeline loader. The struct
// field names and yaml tags are copied verbatim so the JSON wire shape is
// byte-compatible with the Core's type (the subprocess contract is
// preserved unchanged — ADR-025 §3.4). These types are data payloads
// only; this package contains no pipeline execution engine. Validation of
// the definitions is the Core's pipeline loader's responsibility.
package pipeline

// PipelineDefinition is the top-level YAML structure for pipeline files.
type PipelineDefinition struct {
	Pipeline Pipeline `yaml:"pipeline"`
}

// Pipeline represents a complete workflow with one or more PipelineStages.
type Pipeline struct {
	Name   string            `yaml:"name"`
	Stages []PipelineStage   `yaml:"stages"`
	Env    map[string]string `yaml:"env,omitempty"`
}

// PipelineStage groups Tasks that share an execution context or dependency boundary.
//
// Note: Named PipelineStage to avoid conflict with the lifecycle Stage type
// defined in lifecycle.go.
type PipelineStage struct {
	Name     string `yaml:"name"`
	Parallel bool   `yaml:"parallel,omitempty"`
	Tasks    []Task `yaml:"tasks"`
}

// Task is the atomic unit of execution.
type Task struct {
	Name       string            `yaml:"name"`
	Command    string            `yaml:"command"`
	Args       []string          `yaml:"args,omitempty"`
	WorkingDir string            `yaml:"working_dir,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
	Timeout    string            `yaml:"timeout,omitempty"` // duration string like "30s"

	// Environments holds environment-aware overrides keyed by environment name
	// (e.g., "development", "production"). When set, values override/replace
	// the base fields for that specific environment.
	Environments map[string]TaskOverride `yaml:"environments,omitempty"`

	// Metadata holds optional platform-aware execution metadata
	// (ADR-018). The pointer distinguishes "no metadata" (nil) from an
	// explicit metadata block, and omitempty keeps YAML marshaling
	// byte-compatible with existing templates and pipeline files.
	//
	// Reference: TS-P7-23, ADR-018
	Metadata *TaskMetadata `yaml:"metadata,omitempty"`
}

// TaskMetadata declares platform-aware execution metadata for a Task
// (ADR-018, TS-P7-23/24). The pipeline engine uses it to decide which
// build targets can run on the current platform and to support --target
// selection.
type TaskMetadata struct {
	// Platforms lists the platforms that support the task (canonical
	// identifiers "linux", "darwin", "windows"). When non-empty and the
	// current platform is not listed, the task is skipped with a warning
	// — or fails the pipeline in strict mode (--strict, TS-P7-24).
	Platforms []string `yaml:"platforms,omitempty"`

	// Target names the build target the task produces (e.g. "web",
	// "apk", "ios"). It is used by --target selection; when empty, the
	// task name is the fallback target.
	Target string `yaml:"target,omitempty"`
}

// TaskOverride allows environment-specific overrides for a Task.
type TaskOverride struct {
	Command    string            `yaml:"command,omitempty"`
	Args       []string          `yaml:"args,omitempty"`
	WorkingDir string            `yaml:"working_dir,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
	Timeout    string            `yaml:"timeout,omitempty"`
}
