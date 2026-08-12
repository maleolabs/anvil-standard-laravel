// Manifest command metadata of the Laravel adapter (TS-P7-15, TS-P7-16).
//
// The artifact manifest stores the full activation and rollback command
// strings as deployment metadata (ADR-017): the orchestrator — Anvil or
// an external runner — reads them from the manifest and executes them
// during release activation and rollback. These commands mirror the
// operations of the activation phase table (activation.go); the manifest
// strings are the metadata form, the phase table is the executable
// behavior.
//
// Core never imports this package (ADR-009 §8.1): the command values are
// provided to the generic packaging mechanism (artifact.PackageOptions)
// by the CLI wiring layer at packaging time.
package laravel

// ActivationCommands returns the Laravel activation commands in
// execution order: database migration first, then cache warming
// (config, routes, views), then the queue restart signal (TS-018-01-01).
// The full commands (including the `php artisan` prefix) are stored in
// the artifact manifest per ADR-017 and executed by the orchestrator
// during release activation.
//
// The cache form diverges from the executable activation phase table by
// design (view:cache here, event:cache in the table — TD-012); the queue
// restart command appears in both surfaces, always last.
//
// Reference: TS-P7-15 AC-1..AC-4, TS-018-01-01, ADR-017
func ActivationCommands() []string {
	return []string{
		"php artisan migrate --force",
		"php artisan config:cache",
		"php artisan route:cache",
		"php artisan view:cache",
		"php artisan queue:restart",
	}
}

// RollbackCommands returns the Laravel rollback commands in execution
// order. The full command string is stored in the artifact manifest per
// ADR-017 and executed by the orchestrator during release rollback.
//
// The migrate rollback runs force-confirmed (`--force`), mirroring the
// executable rollback phase (activation.go): Laravel's RollbackCommand
// uses ConfirmableTrait and would prompt for confirmation in production —
// the orchestrator executes these strings as non-interactive subprocesses,
// where the default confirmation answer is "no" and the rollback would be
// cancelled.
//
// Reference: TS-P7-16 AC-1..AC-3, ADR-017
func RollbackCommands() []string {
	return []string{
		"php artisan migrate:rollback --force",
	}
}
