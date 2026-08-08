// Verification checks of the Laravel adapter (TS-P7-11, TS-P7-17,
// TS-018-03-01).
//
// Each check validates a Laravel-specific file or structure inside the
// artifact under verification and returns a contracts.VerificationOutcome
// (pass/fail + details), aligned with artifact.CheckResult so outcomes
// merge into the Core's verification report without transformation.
//
// The checks come in two categories (ADR-033 §3, 007 §6): the structural
// checks (the preserved v1.x surface — files and directories exist) and
// the lifecycle-conformity checks (TS-018-03-01) — shared-resource
// wiring, migration timing relative to promotion, queue restart, and
// rollback behavior (Review 19 §3.3). The lifecycle-conformity checks
// are standard-supplied content against the verification contract
// (verification-contract.md, specification corpus): gate semantics and
// evidence requirements belong to the contract; the rules below are the
// Laravel standard's content, additive only — gates are never weakened.
//
// The artifact path may be either a directory (the extracted artifact,
// the common case in tests) or an Anvil artifact archive (tar.gz). For
// archives, entries are scanned directly — no full extraction is
// performed (docs/sessions/impl-TS-P7-09-TS-P7-10-TS-P7-11-TS-P7-12-
// 20260801/CONTEXT.md §Known Risks).
package laravel

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"maleolabs.com/anvil-standard-laravel/internal/contracts"
)

// Verification check names declared in the capability declaration
// (Capabilities). Check names are part of the adapter's contract surface:
// the Core invokes only declared checks (TS-P7-08 AC-3).
//
// Reference: TS-P7-11, TS-P7-17
const (
	// CheckVendorPresent validates that vendor/autoload.php exists —
	// Composer dependencies are installed and autoloadable.
	CheckVendorPresent = "vendor_present"

	// CheckBootstrapStructure validates that bootstrap/app.php exists —
	// the application bootstrap is present and structured.
	CheckBootstrapStructure = "bootstrap_structure"

	// CheckConfigFiles validates that required configuration files exist:
	// config/app.php and .env.example.
	CheckConfigFiles = "config_files"

	// CheckArtisanFile validates that the artisan CLI entrypoint exists
	// in the project root.
	CheckArtisanFile = "artisan_file"

	// CheckComposerJSON validates that composer.json exists in the
	// project root.
	CheckComposerJSON = "composer_json"

	// CheckEnvFile validates that the environment file exists — either
	// .env or .env.example passes the check.
	CheckEnvFile = "env_file"

	// CheckAppDirectory validates that the app/ directory exists in the
	// project root.
	CheckAppDirectory = "app_directory"

	// CheckRoutesDirectory validates that the routes/ directory exists
	// in the project root.
	CheckRoutesDirectory = "routes_directory"

	// CheckSharedResourceWiring validates the shared-resource wiring of
	// the release: the cache store the release declares (CACHE_STORE in
	// .env, else the config/cache.php default) must be the store the
	// release runs with (the compiled config cache default after
	// activation) and must be wired in the release's config/cache.php
	// stores map (TS-018-03-01: shared-resource wiring, Review 19
	// §3.3).
	CheckSharedResourceWiring = "shared_resource_wiring"

	// CheckMigrationTiming validates the migration-timing evidence of
	// the release: the declared post-promotion migration phase (the
	// first activation phase, MigrationTiming) needs the migration set
	// at the declared migrations path and the post-activation state the
	// compiled config cache records (TS-018-03-01: migration timing
	// relative to promotion, Review 19 §3.3).
	CheckMigrationTiming = "migration_timing"

	// CheckQueueRestart validates the queue-restart evidence of the
	// release: with the file cache store (the standard's declared
	// default shared store) the queue:restart signal is a file in the
	// release's file cache store; with any other store the signal lives
	// in the shared store, external to the release directory
	// (TS-018-03-01: queue restart, Review 19 §3.3).
	CheckQueueRestart = "queue_restart"

	// CheckRollbackBehavior validates that rollback produces the
	// declared state: every activation phase declares its rollback
	// coverage (a rollback command when reversible, the irreversible
	// marker when not), the migration rollback is the declared
	// force-confirmed `migrate:rollback --force`, and the manifest
	// rollback metadata matches the executable phase table
	// (TS-018-03-01: rollback behavior, Review 19 §3.3).
	CheckRollbackBehavior = "rollback_behavior"
)

// RunVerification executes one verification check against the artifact
// path and returns the pass/fail outcome.
//
// Reference: TS-P7-11 AC-1..AC-5, TS-P7-17 AC-1..AC-3
func RunVerification(req contracts.VerificationRequest) contracts.VerificationOutcome {
	switch req.Check {
	case CheckVendorPresent:
		return checkFiles(req.ArtifactPath, CheckVendorPresent,
			"vendor/autoload.php")
	case CheckBootstrapStructure:
		return checkFiles(req.ArtifactPath, CheckBootstrapStructure,
			"bootstrap/app.php")
	case CheckConfigFiles:
		return checkFiles(req.ArtifactPath, CheckConfigFiles,
			"config/app.php", ".env.example")
	case CheckArtisanFile:
		return checkFiles(req.ArtifactPath, CheckArtisanFile, "artisan")
	case CheckComposerJSON:
		return checkFiles(req.ArtifactPath, CheckComposerJSON, "composer.json")
	case CheckEnvFile:
		return checkAnyFiles(req.ArtifactPath, CheckEnvFile,
			"neither .env nor .env.example found", ".env", ".env.example")
	case CheckAppDirectory:
		return checkDirectory(req.ArtifactPath, CheckAppDirectory, "app")
	case CheckRoutesDirectory:
		return checkDirectory(req.ArtifactPath, CheckRoutesDirectory, "routes")
	case CheckSharedResourceWiring:
		return checkSharedResourceWiring(req.ArtifactPath)
	case CheckMigrationTiming:
		return checkMigrationTiming(req.ArtifactPath)
	case CheckQueueRestart:
		return checkQueueRestart(req.ArtifactPath)
	case CheckRollbackBehavior:
		return checkRollbackBehavior(req.ArtifactPath)
	default:
		return contracts.VerificationOutcome{
			Name:    req.Check,
			Passed:  false,
			Details: fmt.Sprintf("unknown verification check %q", req.Check),
		}
	}
}

// checkFiles verifies that every required relative path exists in the
// artifact (directory or archive) and reports a single outcome for the
// check.
func checkFiles(artifactPath, checkName string, required ...string) contracts.VerificationOutcome {
	var missing []string
	for _, rel := range required {
		found, err := artifactContains(artifactPath, rel)
		if err != nil {
			return contracts.VerificationOutcome{
				Name:    checkName,
				Passed:  false,
				Details: fmt.Sprintf("cannot inspect artifact %q: %v", artifactPath, err),
			}
		}
		if !found {
			missing = append(missing, rel)
		}
	}

	if len(missing) > 0 {
		return contracts.VerificationOutcome{
			Name:    checkName,
			Passed:  false,
			Details: fmt.Sprintf("missing required file(s): %s", strings.Join(missing, ", ")),
		}
	}
	return contracts.VerificationOutcome{
		Name:    checkName,
		Passed:  true,
		Details: fmt.Sprintf("required file(s) found: %s", strings.Join(required, ", ")),
	}
}

// checkAnyFiles verifies that at least one of the candidate relative
// paths exists in the artifact (directory or archive) and reports a
// single outcome for the check. It is used for checks that accept
// alternatives, such as .env OR .env.example.
func checkAnyFiles(artifactPath, checkName, missingDetail string, candidates ...string) contracts.VerificationOutcome {
	for _, rel := range candidates {
		found, err := artifactContains(artifactPath, rel)
		if err != nil {
			return contracts.VerificationOutcome{
				Name:    checkName,
				Passed:  false,
				Details: fmt.Sprintf("cannot inspect artifact %q: %v", artifactPath, err),
			}
		}
		if found {
			return contracts.VerificationOutcome{
				Name:    checkName,
				Passed:  true,
				Details: fmt.Sprintf("found at least one of: %s", strings.Join(candidates, ", ")),
			}
		}
	}
	return contracts.VerificationOutcome{
		Name:    checkName,
		Passed:  false,
		Details: missingDetail,
	}
}

// checkDirectory verifies that a required directory exists inside the
// artifact (directory or archive) and reports a single outcome for the
// check.
func checkDirectory(artifactPath, checkName, requiredDir string) contracts.VerificationOutcome {
	found, err := artifactContainsDir(artifactPath, requiredDir)
	if err != nil {
		return contracts.VerificationOutcome{
			Name:    checkName,
			Passed:  false,
			Details: fmt.Sprintf("cannot inspect artifact %q: %v", artifactPath, err),
		}
	}
	if !found {
		return contracts.VerificationOutcome{
			Name:    checkName,
			Passed:  false,
			Details: fmt.Sprintf("missing required directory: %s", requiredDir),
		}
	}
	return contracts.VerificationOutcome{
		Name:    checkName,
		Passed:  true,
		Details: fmt.Sprintf("required directory found: %s", requiredDir),
	}
}

// artifactContains reports whether relPath exists inside the artifact at
// artifactPath. When artifactPath is a directory, the path is resolved
// directly; otherwise the path is treated as a tar.gz archive and its
// entries are scanned. Anvil artifact archives store deployable content
// under the "app/" prefix (artifact.DeployableContentDir); both prefixed
// and unprefixed entries are accepted so plain directories and archives
// behave consistently.
func artifactContains(artifactPath, relPath string) (bool, error) {
	info, err := os.Stat(artifactPath)
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(artifactPath, relPath)); err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	return archiveContains(artifactPath, relPath)
}

// artifactContainsDir reports whether the directory relPath exists inside
// the artifact at artifactPath (directory or tar.gz archive). Unlike
// artifactContains, a matching entry must be a directory, not a file.
func artifactContainsDir(artifactPath, relPath string) (bool, error) {
	info, err := os.Stat(artifactPath)
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		dirInfo, err := os.Stat(filepath.Join(artifactPath, relPath))
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		return dirInfo.IsDir(), nil
	}
	return archiveContainsDir(artifactPath, relPath)
}

// scanArchive opens the tar.gz archive and calls match for each entry —
// with the optional "app/" deployable-content prefix stripped from the
// entry name — until match returns true. The returned bool reports
// whether any entry matched.
func scanArchive(archivePath string, match func(name string, hdr *tar.Header) bool) (bool, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return false, fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, err
		}
		if match(strings.TrimPrefix(hdr.Name, "app/"), hdr) {
			return true, nil
		}
	}
	return false, nil
}

// archiveContains scans the entries of a tar.gz archive for relPath,
// accepting an optional "app/" prefix on entry names. Only regular file
// entries (tar.TypeReg) count.
func archiveContains(archivePath, relPath string) (bool, error) {
	return scanArchive(archivePath, func(name string, hdr *tar.Header) bool {
		return name == relPath && hdr.Typeflag == tar.TypeReg
	})
}

// archiveContainsDir scans the entries of a tar.gz archive for evidence
// of the directory relPath. Two forms count as evidence:
//
//  1. An explicit directory entry: tar.TypeDir entries, or regular file
//     entries whose name ends with "/" (some tar writers store
//     directories that way).
//  2. A regular entry living beneath the directory — Anvil artifact
//     archives never contain directory entries (packaging only stores
//     regular files), so a directory is present whenever an entry is
//     stored under its path.
//
// The optional "app/" deployable-content prefix is stripped before
// matching, so prefixed and unprefixed archives behave consistently.
func archiveContainsDir(archivePath, relPath string) (bool, error) {
	return scanArchive(archivePath, func(name string, hdr *tar.Header) bool {
		isDirMarker := hdr.Typeflag == tar.TypeDir ||
			(hdr.Typeflag == tar.TypeReg && strings.HasSuffix(name, "/"))
		if isDirMarker && strings.TrimSuffix(name, "/") == relPath {
			return true
		}
		return strings.HasPrefix(name, relPath+"/")
	})
}

// ---------------------------------------------------------------------------
// Lifecycle-conformity checks (TS-018-03-01, ADR-033 §3, Review 19 §3.3).
//
// The four lifecycle-conformity checks are standard-supplied content
// against the verification contract (verification-contract.md,
// specification corpus): gate semantics and evidence requirements are the
// contract's; the rules below are the Laravel standard's. Evidence is
// re-checkable — it is embedded in the release artifact (the compiled
// config cache, the migration set, the file-cache restart signal, the
// declared phase table) so any consumer can re-run the check and derive
// the same outcome, and outcomes merge into the runtime's verification
// report in the standard outcome shape (name, passed, details).
// ---------------------------------------------------------------------------

// queueRestartCacheKey is the cache key `php artisan queue:restart`
// writes its restart timestamp to (Laravel's QueueRestart signal, keyed
// per queue connection).
const queueRestartCacheKey = "laravel_database_queues_restart"

// queueRestartSignalPath returns the file-cache path of the queue restart
// signal inside the release, derived the way Laravel's FileStore::path
// derives it: the sha1 of the cache key, hex-encoded, split into two
// two-character directory levels, the file named by the full hash, under
// storage/framework/cache/data (the Laravel default path of the file
// cache store).
func queueRestartSignalPath() string {
	sum := sha1.Sum([]byte(queueRestartCacheKey))
	h := hex.EncodeToString(sum[:])
	return filepath.Join("storage", "framework", "cache", "data", h[:2], h[2:4], h)
}

// passOutcome and failOutcome build the standard outcome shape with the
// check name and a details string.
func passOutcome(checkName, details string) contracts.VerificationOutcome {
	return contracts.VerificationOutcome{Name: checkName, Passed: true, Details: details}
}

func failOutcome(checkName, details string) contracts.VerificationOutcome {
	return contracts.VerificationOutcome{Name: checkName, Passed: false, Details: details}
}

// cacheStoreEvidence is the set of cache-store facts the
// lifecycle-conformity checks read from the release artifact.
type cacheStoreEvidence struct {
	// envStore is the CACHE_STORE value declared in .env ("" when
	// absent or empty).
	envStore string

	// declaredStore is the store the release declares: the .env
	// CACHE_STORE value when set, else the config/cache.php default
	// (the env() fallback or the literal default).
	declaredStore string

	// runtimeUsedStore is the store the release runs with: the compiled
	// config cache default (bootstrap/cache/config.php — written by the
	// config:cache activation phase) when present, else declaredStore.
	runtimeUsedStore string

	// stores are the store keys declared in the config/cache.php stores
	// map — the wiring the release ships.
	stores []string

	// compiledPresent reports that bootstrap/cache/config.php exists —
	// the release carries the post-activation compiled configuration.
	compiledPresent bool
}

// resolveCacheStoreEvidence reads the cache-store evidence of the release
// artifact: the declared store, the store the release runs with, and the
// stores wired in config/cache.php. A missing config/cache.php or an
// unreadable artifact is an error — the evidence cannot be re-checked, so
// the checks fail closed.
func resolveCacheStoreEvidence(artifactPath string) (cacheStoreEvidence, error) {
	var ev cacheStoreEvidence

	envData, found, err := artifactReadFile(artifactPath, ".env")
	if err != nil {
		return ev, fmt.Errorf("cannot inspect .env: %v", err)
	}
	if found {
		ev.envStore = envValue(envData, "CACHE_STORE")
	}

	configData, found, err := artifactReadFile(artifactPath, "config/cache.php")
	if err != nil {
		return ev, fmt.Errorf("cannot inspect config/cache.php: %v", err)
	}
	if !found {
		return ev, fmt.Errorf("config/cache.php is missing from the release: the cache-store evidence cannot be re-checked")
	}
	ev.declaredStore = configDefaultStore(configData)
	if ev.envStore != "" {
		ev.declaredStore = ev.envStore
	}
	ev.stores = configStores(configData)

	compiledData, found, err := artifactReadFile(artifactPath, "bootstrap/cache/config.php")
	if err != nil {
		return ev, fmt.Errorf("cannot inspect bootstrap/cache/config.php: %v", err)
	}
	if found {
		ev.compiledPresent = true
		ev.runtimeUsedStore = compiledConfigCacheDefault(compiledData)
	}
	if ev.runtimeUsedStore == "" {
		ev.runtimeUsedStore = ev.declaredStore
	}
	return ev, nil
}

// envValue returns the value of the first KEY=VALUE assignment in a
// Laravel .env file. Lines are "KEY=VALUE"; surrounding single or double
// quotes are stripped; an empty or commented value counts as unset. The
// parser is deliberately minimal — it reads the CACHE_STORE declaration,
// not the full Dotenv grammar.
func envValue(data []byte, key string) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 || strings.TrimSpace(line[:eq]) != key {
			continue
		}
		value := strings.TrimSpace(line[eq+1:])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		return value
	}
	return ""
}

// configDefaultStore returns the default cache store declared in a
// Laravel config/cache.php: the env() fallback when the default is
// written as `'default' => env('CACHE_STORE', 'fallback')`, else the
// literal default `'default' => 'store'`. Empty when the file declares
// no default or declares `env('CACHE_STORE')` without a fallback — the
// store is then undeterminable and the checks fail closed.
func configDefaultStore(data []byte) string {
	text := string(data)
	if m := configDefaultEnvPattern.FindStringSubmatch(text); m != nil {
		return m[2]
	}
	if m := configDefaultLiteralPattern.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// compiledConfigCacheDefault returns the resolved cache default from the
// compiled config cache (bootstrap/cache/config.php — written by
// `php artisan config:cache`). The compiled file nests every config
// file's keys under its own section (`'cache' => array ( 'default' =>
// 'file', ... )`), so the value is read from the cache section only —
// other sections (e.g. database) also carry a top-level 'default' key
// and must not shadow it.
func compiledConfigCacheDefault(data []byte) string {
	text := string(data)
	idx := cacheSectionPattern.FindStringIndex(text)
	if idx == nil {
		return ""
	}
	if m := configDefaultLiteralPattern.FindStringSubmatch(text[idx[0]:]); m != nil {
		return m[1]
	}
	return ""
}

// configStores returns the store keys declared in the stores map of a
// Laravel config/cache.php: every `'name' => [` entry inside the
// `'stores' => [` block, in declaration order. The scan is bracket-aware
// so store driver definitions nested in their own arrays do not confuse
// the block boundary; the shipped Laravel config/cache.php shape is
// standard content (the file Laravel ships for every supported
// version).
func configStores(data []byte) []string {
	text := string(data)
	idx := strings.Index(text, "'stores'")
	if idx < 0 {
		return nil
	}
	open := strings.Index(text[idx:], "[")
	if open < 0 {
		return nil
	}
	block := text[idx+open:]

	// Find the bracket that closes the stores map.
	depth := 0
	closeIdx := -1
	for i := 0; i < len(block); i++ {
		switch block[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		return nil
	}
	block = block[:closeIdx]

	// Store entries are quoted tokens followed by "=> [" at depth 1.
	depth = 0
	var stores []string
	for i := 0; i < len(block); i++ {
		switch block[i] {
		case '[':
			depth++
		case ']':
			depth--
		case '\'', '"':
			quote := block[i]
			j := i + 1
			for j < len(block) && block[j] != quote {
				j++
			}
			token := block[i+1 : j]
			k := j + 1
			for k < len(block) && isConfigSpace(block[k]) {
				k++
			}
			if depth == 1 && k+1 < len(block) && block[k] == '=' && block[k+1] == '>' {
				k += 2
				for k < len(block) && isConfigSpace(block[k]) {
					k++
				}
				if k < len(block) && block[k] == '[' {
					stores = append(stores, token)
				}
			}
			i = j
		}
	}
	return stores
}

// isConfigSpace reports whether b is a whitespace byte a PHP config file
// may use around tokens.
func isConfigSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

var (
	// configDefaultEnvPattern matches `'default' => env('CACHE_STORE',
	// 'fallback')` (the fallback group is empty when absent).
	configDefaultEnvPattern = regexp.MustCompile(`'default'\s*=>\s*env\(\s*'([A-Za-z0-9_]+)'\s*(?:,\s*'([^']*)')?\s*\)`)
	// configDefaultLiteralPattern matches `'default' => 'store'` — the
	// shape of both the literal default in config/cache.php and the
	// resolved default in the compiled config cache.
	configDefaultLiteralPattern = regexp.MustCompile(`'default'\s*=>\s*'([^']+)'`)
	// cacheSectionPattern matches the start of the cache section in the
	// compiled config cache: `'cache' => array (` (Laravel compiles with
	// var_export) or `'cache' => [`.
	cacheSectionPattern = regexp.MustCompile(`'cache'\s*=>\s*(?:array\s*\(|\[)`)
)

// checkSharedResourceWiring implements CheckSharedResourceWiring: the
// store the release declares must be the store the release runs with,
// and the runtime store must be wired in the release's config/cache.php
// stores map. A declared store that drifts from the compiled config
// cache (bootstrap/cache/config.php) is a mis-wired release: after
// config:cache, Laravel serves the compiled value, not .env — the
// classic "declared redis, running file" incident.
//
// Reference: TS-018-03-01, Review 19 §3.3
func checkSharedResourceWiring(artifactPath string) contracts.VerificationOutcome {
	ev, err := resolveCacheStoreEvidence(artifactPath)
	if err != nil {
		return failOutcome(CheckSharedResourceWiring, err.Error())
	}

	if ev.compiledPresent && ev.envStore != "" && ev.runtimeUsedStore != ev.envStore {
		return failOutcome(CheckSharedResourceWiring, fmt.Sprintf(
			"declared cache store %q (CACHE_STORE in .env) does not match the store the release runs with (%q in bootstrap/cache/config.php): the shared resource is not wired as declared — re-check .env against the compiled configuration and re-run the activation",
			ev.envStore, ev.runtimeUsedStore))
	}

	if !slices.Contains(cacheStoreDrivers, ev.runtimeUsedStore) {
		return failOutcome(CheckSharedResourceWiring, fmt.Sprintf(
			"runtime cache store %q is not a known Laravel cache store (expected one of: %s)",
			ev.runtimeUsedStore, strings.Join(cacheStoreDrivers, ", ")))
	}

	if !slices.Contains(ev.stores, ev.runtimeUsedStore) {
		return failOutcome(CheckSharedResourceWiring, fmt.Sprintf(
			"runtime cache store %q is not wired for the release: no %q entry in the config/cache.php stores map (stores: %s)",
			ev.runtimeUsedStore, ev.runtimeUsedStore, strings.Join(ev.stores, ", ")))
	}

	return passOutcome(CheckSharedResourceWiring, fmt.Sprintf(
		"shared cache store wired for the release: runtime store %q matches the declared store %q and is present in the config/cache.php stores map (stores: %s)",
		ev.runtimeUsedStore, ev.declaredStore, strings.Join(ev.stores, ", ")))
}

// checkMigrationTiming implements CheckMigrationTiming: re-checkable
// evidence that migrations ran at the declared post-promotion timing.
// The standard declares migrations as the first activation phase, at
// post-promotion timing (MigrationTiming), followed by the config:cache
// phase — so the compiled config cache is the release's post-activation
// marker, and the migration set at the declared migrations path is what
// the phase applies.
//
// Reference: TS-018-03-01, Review 19 §3.3, TS-018-01-01
func checkMigrationTiming(artifactPath string) contracts.VerificationOutcome {
	_, found, err := artifactReadFile(artifactPath, "bootstrap/cache/config.php")
	if err != nil {
		return failOutcome(CheckMigrationTiming,
			fmt.Sprintf("cannot inspect bootstrap/cache/config.php: %v", err))
	}
	if !found {
		return failOutcome(CheckMigrationTiming,
			"no compiled config cache (bootstrap/cache/config.php): the release carries no post-activation state, so the declared post-promotion migration phase (activation phase 1) has no re-checkable evidence here")
	}

	migrationsDir := "database/migrations"
	dirFound, err := artifactContainsDir(artifactPath, migrationsDir)
	if err != nil {
		return failOutcome(CheckMigrationTiming,
			fmt.Sprintf("cannot inspect %s: %v", migrationsDir, err))
	}
	if !dirFound {
		return failOutcome(CheckMigrationTiming, fmt.Sprintf(
			"declared migrations path %s is missing from the release: the post-promotion migration phase would apply nothing — migration timing evidence is absent (migrations appear stripped from the release)",
			migrationsDir))
	}

	files, err := artifactListFiles(artifactPath, migrationsDir)
	if err != nil {
		return failOutcome(CheckMigrationTiming,
			fmt.Sprintf("cannot list %s: %v", migrationsDir, err))
	}
	var invalid []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".php") {
			invalid = append(invalid, f)
		}
	}
	if len(invalid) > 0 {
		return failOutcome(CheckMigrationTiming, fmt.Sprintf(
			"migration set not well-formed: non-migration file(s) in %s: %s",
			migrationsDir, strings.Join(invalid, ", ")))
	}

	return passOutcome(CheckMigrationTiming, fmt.Sprintf(
		"post-promotion migration timing evidence present: %d migration file(s) at %s, compiled config cache present (bootstrap/cache/config.php); declared timing: %s",
		len(files), migrationsDir, MigrationTiming()))
}

// checkQueueRestart implements CheckQueueRestart: re-checkable evidence
// that the queue was restarted after activation. `php artisan
// queue:restart` (the last declared activation phase) writes the restart
// signal into the shared cache store under the key
// laravel_database_queues_restart. With the file store — the standard's
// declared default shared store (framework.laravel.cache.store) — the
// signal is a file inside the release's file cache store and is verified
// directly; with any other store the signal lives in that shared store,
// external to the release directory, and the check verifies the store
// determination and declares the evidence location.
//
// Reference: TS-018-03-01, Review 19 §3.3, TS-018-01-01
func checkQueueRestart(artifactPath string) contracts.VerificationOutcome {
	ev, err := resolveCacheStoreEvidence(artifactPath)
	if err != nil {
		return failOutcome(CheckQueueRestart, err.Error())
	}
	if ev.runtimeUsedStore == "" {
		return failOutcome(CheckQueueRestart,
			"cannot determine the cache store of the release: no CACHE_STORE in .env and no default in config/cache.php")
	}

	if ev.runtimeUsedStore != "file" {
		return passOutcome(CheckQueueRestart, fmt.Sprintf(
			"cache store is %q: the queue restart signal lives in the shared %q store under key %q, external to the release directory (artifact-embedded evidence applies to the file store); the recorded queue_restart activation outcome is the runtime's lifecycle evidence and the signal is re-checkable in the shared store",
			ev.runtimeUsedStore, ev.runtimeUsedStore, queueRestartCacheKey))
	}

	signalPath := queueRestartSignalPath()
	found, err := artifactContains(artifactPath, signalPath)
	if err != nil {
		return failOutcome(CheckQueueRestart,
			fmt.Sprintf("cannot inspect queue restart signal at %s: %v", signalPath, err))
	}
	if !found {
		return failOutcome(CheckQueueRestart, fmt.Sprintf(
			"no queue restart signal in the file cache store: expected evidence at %s (the Laravel FileStore path of key %q) — the queue was not restarted after activation",
			signalPath, queueRestartCacheKey))
	}

	return passOutcome(CheckQueueRestart, fmt.Sprintf(
		"queue restart signal present at %s (file cache store, key %q) — the queue was restarted after activation",
		signalPath, queueRestartCacheKey))
}

// checkRollbackBehavior implements CheckRollbackBehavior: rollback
// produces the declared state. Every activation phase must declare its
// rollback coverage — a reversible phase carries a rollback command, an
// irreversible phase is marked irreversible with no command (the adapter
// reports an informational success that never blocks rollback, TS-P7-10
// AC-2); the migration rollback must be the declared force-confirmed
// `migrate:rollback --force` (non-interactive production rollback would
// otherwise be cancelled); and the manifest rollback metadata must match
// the executable phase table.
//
// Reference: TS-018-03-01, Review 19 §3.3, TS-018-01-01
func checkRollbackBehavior(artifactPath string) contracts.VerificationOutcome {
	if _, err := os.Stat(artifactPath); err != nil {
		return failOutcome(CheckRollbackBehavior,
			fmt.Sprintf("cannot inspect artifact %q: %v", artifactPath, err))
	}

	for _, p := range phases {
		if p.irreversible {
			if len(p.rollbackArgs) > 0 {
				return failOutcome(CheckRollbackBehavior, fmt.Sprintf(
					"phase %q is marked irreversible but declares a rollback command (%s): rollback semantics are incoherent",
					p.name, strings.Join(p.rollbackArgs, " ")))
			}
			continue
		}
		if len(p.rollbackArgs) == 0 {
			return failOutcome(CheckRollbackBehavior, fmt.Sprintf(
				"phase %q declares no rollback command and is not marked irreversible: rollback coverage is missing",
				p.name))
		}
	}

	migratePhase, ok := lookupPhase(PhaseMigrate)
	if !ok {
		return failOutcome(CheckRollbackBehavior,
			"phase table declares no migrate phase: rollback cannot produce the declared state")
	}
	wantMigrateRollback := "migrate:rollback --force"
	if got := strings.Join(migratePhase.rollbackArgs, " "); got != wantMigrateRollback {
		return failOutcome(CheckRollbackBehavior, fmt.Sprintf(
			"migrate rollback command %q does not match the declared force-confirmed rollback %q: non-interactive production rollback would be cancelled",
			got, wantMigrateRollback))
	}

	manifestRollback := RollbackCommands()
	if len(manifestRollback) != 1 || manifestRollback[0] != "php artisan "+wantMigrateRollback {
		return failOutcome(CheckRollbackBehavior, fmt.Sprintf(
			"manifest rollback metadata (%s) drifts from the executable phase table (php artisan %s): the surfaces must not diverge",
			strings.Join(manifestRollback, "; "), wantMigrateRollback))
	}

	var coverage []string
	for _, p := range phases {
		if p.irreversible {
			coverage = append(coverage, fmt.Sprintf("%s: informational (irreversible, rollback never blocks)", p.name))
		} else {
			coverage = append(coverage, fmt.Sprintf("%s: php artisan %s", p.name, strings.Join(p.rollbackArgs, " ")))
		}
	}
	return passOutcome(CheckRollbackBehavior, fmt.Sprintf(
		"rollback produces the declared state: %s; manifest rollback metadata matches the phase table",
		strings.Join(coverage, "; ")))
}

// artifactReadFile returns the content of relPath inside the artifact
// (directory or tar.gz archive). The bool reports whether the path
// exists; an unreadable artifact is an error.
func artifactReadFile(artifactPath, relPath string) ([]byte, bool, error) {
	info, err := os.Stat(artifactPath)
	if err != nil {
		return nil, false, err
	}
	if info.IsDir() {
		data, err := os.ReadFile(filepath.Join(artifactPath, relPath))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		return data, true, nil
	}
	return readArchiveEntry(artifactPath, relPath)
}

// readArchiveEntry scans a tar.gz archive for relPath (accepting the
// optional "app/" deployable-content prefix) and returns the content of
// the first matching regular-file entry.
func readArchiveEntry(archivePath, relPath string) ([]byte, bool, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, false, fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
		name := strings.TrimPrefix(hdr.Name, "app/")
		if name == relPath && hdr.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, false, err
			}
			return data, true, nil
		}
	}
	return nil, false, nil
}

// artifactListFiles returns the names of the regular files directly
// inside relDir within the artifact (directory or tar.gz archive).
// Subdirectories are not descended into; for archives the optional
// "app/" prefix is stripped and directory entries are ignored.
func artifactListFiles(artifactPath, relDir string) ([]string, error) {
	info, err := os.Stat(artifactPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(filepath.Join(artifactPath, relDir))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		return names, nil
	}
	return listArchiveFiles(artifactPath, relDir)
}

// listArchiveFiles scans a tar.gz archive for regular files directly
// inside relDir (with the optional "app/" prefix stripped) and returns
// their names, deduplicated.
func listArchiveFiles(archivePath, relDir string) ([]string, error) {
	prefix := relDir + "/"
	seen := map[string]bool{}
	var names []string
	_, err := scanArchive(archivePath, func(name string, hdr *tar.Header) bool {
		if hdr.Typeflag == tar.TypeReg && strings.HasPrefix(name, prefix) {
			rest := strings.TrimPrefix(name, prefix)
			if rest != "" && !strings.Contains(rest, "/") && !seen[rest] {
				seen[rest] = true
				names = append(names, rest)
			}
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}
