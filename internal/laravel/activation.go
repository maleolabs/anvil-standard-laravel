// Activation phase operations of the Laravel adapter (TS-P7-09, TS-P7-10,
// TS-018-01-01).
//
// Each activation phase runs the corresponding `php artisan` command as a
// subprocess from the release's working directory and reports the outcome
// through the activation contract payloads (contracts.ActivationResult).
// The phases are declared in activation order: migrations first (at the
// declared timing relative to promotion), then cache warming (config,
// routes, events — in that declared order), then the queue restart phase
// last, so workers recycle only after the code, migrations, and caches are
// in place.
//
// The rollback operation (PhaseOperationRollback) reverses the reversible
// phases (migrate → migrate:rollback); irreversible phases — the cache
// phases and the queue restart signal, whose effects cannot be undone —
// return an informational success that does not block rollback (TS-P7-10
// AC-2, TS-018-01-01 DoD).
//
// The phase table is the executable single source of truth for the
// Lifecycle Definition (lifecycle/definition.md): phase set, declared
// order, grouping, per-phase failure semantics, and per-phase rollback
// semantics all live here.
package laravel

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"maleolabs.com/anvil-standard-laravel/internal/contracts"
)

// Activation phase names declared in the capability declaration
// (Capabilities). Phase names are part of the adapter's contract surface:
// the Core invokes only declared phases (TS-P7-08 AC-3).
//
// Reference: TS-P7-09, TS-P7-07
const (
	// PhaseMigrate applies database migrations during activation. It is
	// the first declared phase and runs at the declared timing relative
	// to promotion (see MigrationTiming).
	PhaseMigrate = "migrate"

	// PhaseConfigCache caches the application configuration. It is the
	// first cache-warming phase — caches that follow compile against the
	// cached configuration.
	PhaseConfigCache = "config_cache"

	// PhaseRouteCache caches the application routes.
	PhaseRouteCache = "route_cache"

	// PhaseEventCache caches the application events.
	PhaseEventCache = "event_cache"

	// PhaseViewCache caches the application compiled views. It is not
	// part of the executable activation phase table — view:cache is not
	// reversible as a rollback operation, so the activation pipeline
	// cannot include it — but the constant lives with the other artisan
	// cache phases because the build pipeline reuses it (TS-P7-14: the
	// `view:cache` build phase). The executable activation command set —
	// including this exclusion — is the documented contract in MVP-002
	// §3.2 (TD-012); the manifest metadata surface carries `view:cache`
	// (TS-P7-15 AC-3, 005-adapter-command-contract §10.10).
	PhaseViewCache = "view_cache"

	// PhaseQueueRestart recycles the queue workers after activation: it
	// signals every running worker to restart once its current job
	// finishes, so workers pick up the new code, migrations, and caches.
	// It is the last declared phase (TS-018-01-01): worker recycling
	// runs only after migration and cache warming completed, so no
	// worker ever processes a job against stale migrations or caches.
	PhaseQueueRestart = "queue_restart"
)

// phaseGroup classifies the activation phases into the declared groups of
// the Lifecycle Definition (TS-018-01-01). Groups appear in the phase
// table in declaration order: migration, cache warming, queue.
type phaseGroup string

const (
	// PhaseGroupMigration is the database migration phase group.
	PhaseGroupMigration phaseGroup = "migration"

	// PhaseGroupCacheWarming is the cache warming phase group: config,
	// routes, events, in that declared order. Every cache-warming phase
	// is irreversible — a rollback cannot restore the previous release's
	// caches; the previous release's own activation regenerates them
	// from its code.
	PhaseGroupCacheWarming phaseGroup = "cache_warming"

	// PhaseGroupQueue is the queue worker recycling phase group. The
	// restart signal cannot be undone either; rollback documents it as
	// informational, never blocking (TS-018-01-01 DoD).
	PhaseGroupQueue phaseGroup = "queue"
)

// Migration timing relative to promotion (TS-018-01-01). The Laravel
// standard uses the server deployment model (ADR-016): the release
// directory becomes the live application, so the release is promoted to
// active BEFORE the activation phases run (docs/deploy.md §3). Migrations
// therefore run after promotion: the declared timing is post-promotion,
// and a failed migration leaves the release recorded as active — the
// activation is re-run to converge (migrate is idempotent per migration
// record).
const (
	// MigrationTimingPostPromotion declares that the migration phase runs
	// after the release is promoted to active.
	MigrationTimingPostPromotion = "post_promotion"
)

// MigrationTiming returns the declared timing of the migration phase
// relative to release promotion. The value is part of the Lifecycle
// Definition (TS-018-01-01): the standard declares post-promotion
// migrations for the in-place server deployment model.
func MigrationTiming() string {
	return MigrationTimingPostPromotion
}

// phase defines one activation phase: the artisan command it runs during
// activation, its rollback command when reversible, whether the
// operation is irreversible (no rollback possible — the phase reports an
// informational result instead of blocking rollback), and the declared
// failure semantics of the phase.
//
// Reference: TS-P7-09, TS-P7-10, TS-018-01-01
type phase struct {
	// name is the phase identifier (Phase* constants).
	name string

	// group classifies the phase (migration, cache warming, queue).
	group phaseGroup

	// activateArgs are the `php artisan` arguments for the activate
	// operation (e.g. []string{"migrate", "--force"}).
	activateArgs []string

	// rollbackArgs are the `php artisan` arguments for the rollback
	// operation. Nil when the phase has no rollback operation.
	rollbackArgs []string

	// irreversible reports that the phase's effects cannot be undone.
	// Rollback requests then return an informational success instead of
	// running a command (TS-P7-10 AC-2).
	irreversible bool

	// failureSemantics declares the phase's failure semantics: what a
	// failing phase means for the activation and how the state converges
	// on re-execution (TS-018-01-01 DoD).
	failureSemantics string
}

// phases is the adapter's phase table, in capability declaration order
// (TS-018-01-01):
//
//  1. migrate — at the declared post-promotion timing (MigrationTiming);
//  2. cache warming: config_cache → route_cache → event_cache — in the
//     declared order (config first, because route and event caching
//     compile against the cached configuration), each irreversible;
//  3. queue_restart — last, so workers recycle only after migration and
//     cache warming completed; irreversible as a signal.
//
// The cache and queue phases are irreversible: a rollback cannot restore
// the previous release's caches or unsignal its workers — the previous
// release's own activation regenerates both from its code.
//
// Reference: TS-P7-09, TS-P7-10, TS-018-01-01
var phases = []phase{
	{
		name:         PhaseMigrate,
		group:        PhaseGroupMigration,
		activateArgs: []string{"migrate", "--force"},
		rollbackArgs: []string{"migrate:rollback"},
		failureSemantics: "a failing migration fails the activation; the " +
			"release stays recorded as active (post-promotion timing) and " +
			"re-running the activation re-executes the phase — Laravel " +
			"skips migrations already recorded as applied, so re-execution " +
			"converges to the intended schema",
	},
	{
		name:         PhaseConfigCache,
		group:        PhaseGroupCacheWarming,
		activateArgs: []string{"config:cache"},
		irreversible: true,
		failureSemantics: "a failing cache phase fails the activation; " +
			"re-running the activation regenerates the cache from the " +
			"release's code — caches reflect exactly one release and are " +
			"never repaired, only regenerated",
	},
	{
		name:         PhaseRouteCache,
		group:        PhaseGroupCacheWarming,
		activateArgs: []string{"route:cache"},
		irreversible: true,
		failureSemantics: "a failing cache phase fails the activation; " +
			"re-running the activation regenerates the cache from the " +
			"release's code — caches reflect exactly one release and are " +
			"never repaired, only regenerated",
	},
	{
		name:         PhaseEventCache,
		group:        PhaseGroupCacheWarming,
		activateArgs: []string{"event:cache"},
		irreversible: true,
		failureSemantics: "a failing cache phase fails the activation; " +
			"re-running the activation regenerates the cache from the " +
			"release's code — caches reflect exactly one release and are " +
			"never repaired, only regenerated",
	},
	{
		name:         PhaseQueueRestart,
		group:        PhaseGroupQueue,
		activateArgs: []string{"queue:restart"},
		irreversible: true,
		failureSemantics: "a failing queue restart fails the activation; " +
			"re-running the activation re-sends the restart signal — " +
			"worker recycling is idempotent, workers already running the " +
			"new release simply receive the signal again",
	},
}

// commandRunner executes one artisan command: `php artisan <args...>`
// in dir (empty dir = current working directory) and returns the command
// output. The runner is a function so tests can inject a fake
// implementation without requiring PHP on the host.
//
// Reference: TS-P7-09 §7
type commandRunner func(ctx context.Context, dir string, args ...string) (output string, err error)

// runArtisan is the production command runner: it executes
// `php artisan <args...>` via os/exec with the environment inherited and
// the working directory set when dir is non-empty. The adapter is a
// standalone executable (004-review-resolutions D1) — it uses os/exec
// directly, not the Core's Process Runner, which exists only on the Core
// side.
//
// On failure the error carries the artisan stderr (or the exit error when
// stderr is empty) so phase failures report actionable details
// (TS-P7-09 AC-6).
//
// Reference: TS-P7-09, 004-review-resolutions D1
func runArtisan(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "php", append([]string{"artisan"}, args...)...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return stdout.String(), fmt.Errorf("artisan %s failed: %s", strings.Join(args, " "), detail)
	}
	return stdout.String(), nil
}

// RunActivation executes one activation phase operation and returns the
// contract result. The JSON result is authoritative for the phase
// outcome: Success=false with Error details reports a phase failure,
// while the process-level exit code reports whether the adapter itself
// produced a result (005-adapter-command-contract §7).
//
// Reference: TS-P7-09 AC-1..AC-6, TS-P7-10 AC-1, AC-2
func RunActivation(ctx context.Context, runner commandRunner, req contracts.ActivationRequest) contracts.ActivationResult {
	p, ok := lookupPhase(req.Phase)
	if !ok {
		return contracts.ActivationResult{
			Success: false,
			Error:   fmt.Sprintf("unknown activation phase %q", req.Phase),
		}
	}

	switch req.Operation {
	case contracts.PhaseOperationActivate:
		return runPhase(ctx, runner, req.Release.WorkingDir, p, p.activateArgs)
	case contracts.PhaseOperationRollback:
		if p.irreversible {
			return irreversibleRollbackResult(p)
		}
		return runPhase(ctx, runner, req.Release.WorkingDir, p, p.rollbackArgs)
	default:
		return contracts.ActivationResult{
			Success: false,
			Error:   fmt.Sprintf("unknown activation operation %q", req.Operation),
		}
	}
}

// lookupPhase returns the phase definition for the given name.
func lookupPhase(name string) (phase, bool) {
	for _, p := range phases {
		if p.name == name {
			return p, true
		}
	}
	return phase{}, false
}

// runPhase executes the given artisan arguments via the runner and maps
// the outcome to an ActivationResult. The working directory from the
// release context is passed through so artisan runs inside the release
// directory (005-adapter-command-contract §3.3).
func runPhase(ctx context.Context, runner commandRunner, dir string, p phase, args []string) contracts.ActivationResult {
	output, err := runner(ctx, dir, args...)
	if err != nil {
		return contracts.ActivationResult{
			Success: false,
			Output:  output,
			Error:   err.Error(),
		}
	}
	return contracts.ActivationResult{
		Success: true,
		Output:  output,
	}
}

// irreversibleRollbackResult reports an informational success for a
// rollback request on an irreversible phase. The operation cannot be
// undone, so the adapter documents the limitation in the result and does
// NOT block the rollback (TS-P7-10 AC-2): the Core treats Success=true as
// "rollback of this phase completed without error".
func irreversibleRollbackResult(p phase) contracts.ActivationResult {
	return contracts.ActivationResult{
		Success: true,
		Output: fmt.Sprintf(
			"phase %q is irreversible: %s cannot be undone; rollback proceeds without undoing this operation",
			p.name, strings.Join(p.activateArgs, " "),
		),
	}
}
