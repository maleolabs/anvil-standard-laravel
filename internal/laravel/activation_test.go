// Tests for the Laravel adapter activation phases (TS-P7-09) and
// rollback operations (TS-P7-10). A fake command runner is injected in
// place of the production `php artisan` execution, so no PHP is required
// on the test host.
package laravel

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"maleolabs.com/anvil-standard-laravel/internal/contracts"
)

// fakeRunner records the invocations it received and returns the given
// output and error. It implements commandRunner for tests.
type fakeRunner struct {
	dirs   []string
	args   [][]string
	output string
	err    error
}

func (f *fakeRunner) run(_ context.Context, dir string, args ...string) (string, error) {
	f.dirs = append(f.dirs, dir)
	f.args = append(f.args, args)
	return f.output, f.err
}

// request builds an ActivationRequest for the given phase and operation
// with the release context carrying workingDir.
func request(phase string, operation contracts.PhaseOperation, workingDir string) contracts.ActivationRequest {
	return contracts.ActivationRequest{
		Phase:     phase,
		Operation: operation,
		Release: contracts.ReleaseContext{
			ProjectID:  "acme-shop",
			ReleaseID:  "rel-20260801-01",
			WorkingDir: workingDir,
		},
	}
}

// TestRunActivation_ActivateMigrate verifies that the migrate phase runs
// `php artisan migrate --force` in the release working directory and
// reports success with the command output (TS-P7-09 AC-1, AC-5).
func TestRunActivation_ActivateMigrate(t *testing.T) {
	runner := &fakeRunner{output: "Migration table created successfully."}
	workingDir := "/var/www/acme-shop/releases/rel-1"

	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationActivate, workingDir))

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
	if result.Output != "Migration table created successfully." {
		t.Errorf("Output = %q, want the runner output", result.Output)
	}
	if len(runner.args) != 1 {
		t.Fatalf("runner invoked %d time(s), want 1", len(runner.args))
	}
	wantArgs := []string{"migrate", "--force"}
	if !reflect.DeepEqual(runner.args[0], wantArgs) {
		t.Errorf("runner args = %v, want %v", runner.args[0], wantArgs)
	}
	if runner.dirs[0] != workingDir {
		t.Errorf("runner working dir = %q, want %q", runner.dirs[0], workingDir)
	}
}

// TestRunActivation_ActivateAllPhases verifies the artisan command of
// each declared activation phase (TS-P7-09 AC-1..AC-4, TS-018-01-01).
func TestRunActivation_ActivateAllPhases(t *testing.T) {
	tests := []struct {
		phase string
		args  []string
	}{
		{phase: PhaseMigrate, args: []string{"migrate", "--force"}},
		{phase: PhaseConfigCache, args: []string{"config:cache"}},
		{phase: PhaseRouteCache, args: []string{"route:cache"}},
		{phase: PhaseEventCache, args: []string{"event:cache"}},
		{phase: PhaseQueueRestart, args: []string{"queue:restart"}},
	}
	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			runner := &fakeRunner{output: "ok"}
			result := RunActivation(context.Background(), runner.run, request(tt.phase, contracts.PhaseOperationActivate, ""))
			if !result.Success {
				t.Fatalf("Success = false, want true (result: %#v)", result)
			}
			if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], tt.args) {
				t.Errorf("runner args = %v, want %v", runner.args, tt.args)
			}
		})
	}
}

// TestRunActivation_NoWorkingDir verifies that an empty working directory
// is passed through untouched — the process runs in the adapter's current
// directory.
func TestRunActivation_NoWorkingDir(t *testing.T) {
	runner := &fakeRunner{output: "ok"}
	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationActivate, ""))
	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if runner.dirs[0] != "" {
		t.Errorf("runner working dir = %q, want empty", runner.dirs[0])
	}
}

// TestRunActivation_ActivateFailure verifies that a failing artisan
// command reports failure with the error details while preserving any
// partial output (TS-P7-09 AC-6).
func TestRunActivation_ActivateFailure(t *testing.T) {
	runner := &fakeRunner{
		output: "migrations started",
		err:    fmt.Errorf("artisan migrate --force failed: SQLSTATE[42S02]: table users already exists"),
	}

	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationActivate, "/var/www/app"))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if result.Output != "migrations started" {
		t.Errorf("Output = %q, want the partial runner output", result.Output)
	}
	if !strings.Contains(result.Error, "table users already exists") {
		t.Errorf("Error = %q, want it to carry the artisan failure detail", result.Error)
	}
}

// TestRunActivation_UnknownPhase verifies that an undeclared phase yields
// a failure result without invoking the runner.
func TestRunActivation_UnknownPhase(t *testing.T) {
	runner := &fakeRunner{}
	result := RunActivation(context.Background(), runner.run, request("unknown_phase", contracts.PhaseOperationActivate, ""))

	if result.Success {
		t.Error("Success = true, want false")
	}
	if !strings.Contains(result.Error, `unknown activation phase "unknown_phase"`) {
		t.Errorf("Error = %q, want mention of the unknown phase", result.Error)
	}
	if len(runner.args) != 0 {
		t.Errorf("runner invoked %d time(s), want 0", len(runner.args))
	}
}

// TestRunActivation_UnknownOperation verifies that an unsupported
// operation value yields a failure result.
func TestRunActivation_UnknownOperation(t *testing.T) {
	runner := &fakeRunner{}
	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperation("sideways"), ""))

	if result.Success {
		t.Error("Success = true, want false")
	}
	if !strings.Contains(result.Error, `unknown activation operation "sideways"`) {
		t.Errorf("Error = %q, want mention of the unknown operation", result.Error)
	}
}

// TestRunActivation_RollbackMigrate verifies that the rollback operation
// of the migrate phase runs `php artisan migrate:rollback --force` in the
// release working directory (TS-P7-10 AC-1). The `--force` flag pins the
// exact argument set: Laravel's RollbackCommand uses ConfirmableTrait and
// prompts for confirmation in production — the adapter runs artisan as a
// non-interactive subprocess, so without --force the rollback would always
// be cancelled (confirm defaults to "no" without a TTY).
func TestRunActivation_RollbackMigrate(t *testing.T) {
	runner := &fakeRunner{output: "Rolled back: 2026_07_31_000001"}
	workingDir := "/var/www/acme-shop/releases/rel-0"

	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationRollback, workingDir))

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	wantArgs := []string{"migrate:rollback", "--force"}
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], wantArgs) {
		t.Errorf("runner args = %v, want %v", runner.args, wantArgs)
	}
	if runner.dirs[0] != workingDir {
		t.Errorf("runner working dir = %q, want %q", runner.dirs[0], workingDir)
	}
}

// TestRunActivation_RollbackMigrateFailure verifies that a failing
// migrate:rollback --force reports failure with error details.
func TestRunActivation_RollbackMigrateFailure(t *testing.T) {
	runner := &fakeRunner{err: fmt.Errorf("artisan migrate:rollback --force failed: connection refused")}
	result := RunActivation(context.Background(), runner.run, request(PhaseMigrate, contracts.PhaseOperationRollback, ""))

	if result.Success {
		t.Fatal("Success = true, want false")
	}
	if !strings.Contains(result.Error, "connection refused") {
		t.Errorf("Error = %q, want it to carry the failure detail", result.Error)
	}
}

// TestRunActivation_RollbackIrreversible verifies that cache phases and
// the queue restart phase are irreversible: the rollback operation
// returns an informational success that documents the irreversibility
// and does NOT invoke the runner — the rollback is not blocked (TS-P7-10
// AC-2, TS-018-01-01 DoD).
func TestRunActivation_RollbackIrreversible(t *testing.T) {
	for _, phase := range []string{PhaseConfigCache, PhaseRouteCache, PhaseEventCache, PhaseQueueRestart} {
		t.Run(phase, func(t *testing.T) {
			runner := &fakeRunner{}
			result := RunActivation(context.Background(), runner.run, request(phase, contracts.PhaseOperationRollback, "/var/www/app"))

			if !result.Success {
				t.Fatalf("Success = false, want true — irreversible phases must not block rollback (result: %#v)", result)
			}
			if !strings.Contains(result.Output, "irreversible") {
				t.Errorf("Output = %q, want it to document the irreversibility", result.Output)
			}
			if len(runner.args) != 0 {
				t.Errorf("runner invoked %d time(s), want 0 — no command may run for an irreversible phase", len(runner.args))
			}
		})
	}
}

// TestCapabilities_DeclaresPhases verifies that the capability declaration
// lists exactly the five activation phases in declaration order — the
// Core invokes only declared phases (TS-P7-09 DoD, TS-P7-10 AC-3,
// TS-018-01-01).
func TestCapabilities_DeclaresPhases(t *testing.T) {
	result := Capabilities()
	want := []string{PhaseMigrate, PhaseConfigCache, PhaseRouteCache, PhaseEventCache, PhaseQueueRestart}
	if !reflect.DeepEqual(result.Declaration.ActivationPhases, want) {
		t.Errorf("ActivationPhases = %v, want %v", result.Declaration.ActivationPhases, want)
	}
}

// TestActivation_PhaseTableMatchesDocumentedCommandSet pins the
// executable activation phase table to its documented contract (MVP-002
// §3.2, TS-P7-09, TS-018-01-01): migrate, config:cache, route:cache,
// event:cache and queue:restart, with view:cache excluded because it is
// not reversible as a rollback operation (TS-P7-10). This is the
// executable surface only — the manifest metadata surface
// (`ActivationCommands`, view:cache form) is pinned separately in
// manifest_commands_test.go; the two surfaces deliberately diverge on
// the cache phase (005-adapter-command-contract §10.10, TD-012). The
// test makes the contract visible so a future change to the phase table
// cannot silently diverge from the document again.
func TestActivation_PhaseTableMatchesDocumentedCommandSet(t *testing.T) {
	// The table must contain exactly the five documented activation
	// phases and nothing else.
	names := make([]string, 0, len(phases))
	for _, p := range phases {
		names = append(names, p.name)
	}
	want := []string{PhaseMigrate, PhaseConfigCache, PhaseRouteCache, PhaseEventCache, PhaseQueueRestart}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("activation phase table = %v, want %v", names, want)
	}

	// view_cache must not resolve to an activation phase — it is a build
	// phase (TS-P7-14), not an activation phase (TD-012).
	if _, ok := lookupPhase(PhaseViewCache); ok {
		t.Error("view_cache found in the activation phase table, want excluded")
	}

	// An activation request for view_cache must fail as an unknown phase
	// without invoking the runner.
	runner := &fakeRunner{}
	result := RunActivation(context.Background(), runner.run, request(PhaseViewCache, contracts.PhaseOperationActivate, ""))
	if result.Success {
		t.Fatal("Success = true, want false — view_cache is not an activation phase")
	}
	if !strings.Contains(result.Error, `unknown activation phase "view_cache"`) {
		t.Errorf("Error = %q, want the unknown-phase failure", result.Error)
	}
	if len(runner.args) != 0 {
		t.Errorf("runner invoked %d time(s), want 0", len(runner.args))
	}
}

// TestRunActivation_ActivateQueueRestart verifies that the queue restart
// phase runs `php artisan queue:restart` in the release working directory
// and reports success — worker recycling is the last activation phase
// (TS-018-01-01 DoD: queue restart declared).
func TestRunActivation_ActivateQueueRestart(t *testing.T) {
	runner := &fakeRunner{output: "Queue workers restarted successfully."}
	workingDir := "/var/www/acme-shop/releases/rel-1"

	result := RunActivation(context.Background(), runner.run, request(PhaseQueueRestart, contracts.PhaseOperationActivate, workingDir))

	if !result.Success {
		t.Fatalf("Success = false, want true (result: %#v)", result)
	}
	if result.Output != "Queue workers restarted successfully." {
		t.Errorf("Output = %q, want the runner output", result.Output)
	}
	if len(runner.args) != 1 || !reflect.DeepEqual(runner.args[0], []string{"queue:restart"}) {
		t.Errorf("runner args = %v, want [queue:restart]", runner.args)
	}
	if runner.dirs[0] != workingDir {
		t.Errorf("runner working dir = %q, want %q", runner.dirs[0], workingDir)
	}
}

// TestActivation_DeclaredOrder verifies the declared activation phase
// order (TS-018-01-01 DoD): migrate first (migration group), then the
// cache warming group (config → route → event), then the queue restart
// phase last. The migration phase is first and runs at the declared
// post-promotion timing; the queue phase is last so workers recycle only
// after migration and cache warming completed.
func TestActivation_DeclaredOrder(t *testing.T) {
	// The phase table is the declared order; it must start with the
	// migration group, continue with the cache warming group, and end
	// with the queue group.
	if len(phases) == 0 {
		t.Fatal("phase table is empty")
	}

	// Migration first.
	if phases[0].name != PhaseMigrate || phases[0].group != PhaseGroupMigration {
		t.Errorf("first phase = (%q, %q), want (%q, %q)", phases[0].name, phases[0].group, PhaseMigrate, PhaseGroupMigration)
	}

	// Cache warming group contiguous and in the declared order
	// (config → route → event) immediately after migration.
	wantCacheOrder := []string{PhaseConfigCache, PhaseRouteCache, PhaseEventCache}
	for i, want := range wantCacheOrder {
		p := phases[i+1]
		if p.name != want || p.group != PhaseGroupCacheWarming {
			t.Errorf("phase %d = (%q, %q), want (%q, %q) in the cache warming group", i+1, p.name, p.group, want, PhaseGroupCacheWarming)
		}
	}

	// Queue restart last, after the whole cache warming group.
	last := phases[len(phases)-1]
	if last.name != PhaseQueueRestart || last.group != PhaseGroupQueue {
		t.Errorf("last phase = (%q, %q), want (%q, %q)", last.name, last.group, PhaseQueueRestart, PhaseGroupQueue)
	}
	if len(phases) != 5 {
		t.Errorf("phase table length = %d, want 5", len(phases))
	}
}

// TestActivation_MigrationTimingPostPromotion verifies the declared
// migration timing relative to promotion (TS-018-01-01 DoD): the release
// is promoted to active before the activation phases run (in-place server
// deployment model, ADR-016), so migrations are declared post-promotion.
func TestActivation_MigrationTimingPostPromotion(t *testing.T) {
	if got := MigrationTiming(); got != MigrationTimingPostPromotion {
		t.Errorf("MigrationTiming() = %q, want %q", got, MigrationTimingPostPromotion)
	}
	if MigrationTimingPostPromotion != "post_promotion" {
		t.Errorf("MigrationTimingPostPromotion = %q, want \"post_promotion\"", MigrationTimingPostPromotion)
	}
}

// TestActivation_CacheWarmingOrderAndIrreversibility verifies the
// declared cache warming semantics (TS-018-01-01 DoD): config_cache →
// route_cache → event_cache in that order, each irreversible with no
// rollback command. Ordering matters: route and event caching compile
// against the cached configuration, so config is warmed first.
func TestActivation_CacheWarmingOrderAndIrreversibility(t *testing.T) {
	want := []struct {
		name string
		args []string
	}{
		{name: PhaseConfigCache, args: []string{"config:cache"}},
		{name: PhaseRouteCache, args: []string{"route:cache"}},
		{name: PhaseEventCache, args: []string{"event:cache"}},
	}

	var cachePhases []phase
	for _, p := range phases {
		if p.group == PhaseGroupCacheWarming {
			cachePhases = append(cachePhases, p)
		}
	}

	if len(cachePhases) != len(want) {
		t.Fatalf("cache warming phases = %d, want %d (%v)", len(cachePhases), len(want), cachePhases)
	}
	for i, w := range want {
		p := cachePhases[i]
		if p.name != w.name {
			t.Errorf("cache warming phase %d = %q, want %q", i, p.name, w.name)
		}
		if !reflect.DeepEqual(p.activateArgs, w.args) {
			t.Errorf("phase %q activate args = %v, want %v", p.name, p.activateArgs, w.args)
		}
		// Irreversibility semantics: no rollback command and the
		// irreversible flag set — rollback never blocks on them.
		if !p.irreversible {
			t.Errorf("phase %q is reversible, want irreversible", p.name)
		}
		if len(p.rollbackArgs) != 0 {
			t.Errorf("phase %q declares rollback args %v, want none (irreversible)", p.name, p.rollbackArgs)
		}
	}
}

// TestActivation_FailureSemanticsDeclared verifies that every declared
// activation phase carries non-empty failure semantics (TS-018-01-01
// DoD: per-phase failure semantics declared).
func TestActivation_FailureSemanticsDeclared(t *testing.T) {
	if len(phases) == 0 {
		t.Fatal("phase table is empty")
	}
	for _, p := range phases {
		if strings.TrimSpace(p.failureSemantics) == "" {
			t.Errorf("phase %q has empty failureSemantics", p.name)
		}
	}
}

// TestActivation_RollbackSemanticsPerPhase verifies the declared
// rollback semantics of every phase (TS-018-01-01 DoD): each phase is
// either reversible with a declared rollback command or irreversible
// (no rollback command, rollback reports informational success and never
// blocks). Exactly one of the two applies to every phase.
func TestActivation_RollbackSemanticsPerPhase(t *testing.T) {
	if len(phases) == 0 {
		t.Fatal("phase table is empty")
	}
	for _, p := range phases {
		reversible := len(p.rollbackArgs) > 0
		switch {
		case reversible && p.irreversible:
			t.Errorf("phase %q declares both rollback args and irreversibility, want exactly one", p.name)
		case reversible:
			// The migrate phase carries the only rollback command,
			// force-confirmed for the non-interactive production
			// execution of the adapter.
			if p.name != PhaseMigrate || !reflect.DeepEqual(p.rollbackArgs, []string{"migrate:rollback", "--force"}) {
				t.Errorf("phase %q rollback args = %v, want the migrate:rollback --force command on the migrate phase only", p.name, p.rollbackArgs)
			}
		case !p.irreversible:
			t.Errorf("phase %q has no rollback args and is not irreversible, want exactly one rollback semantic", p.name)
		}
	}
}
