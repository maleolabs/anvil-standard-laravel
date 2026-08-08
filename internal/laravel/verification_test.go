// Tests for the Laravel adapter verification checks (TS-P7-11, TS-P7-17,
// TS-018-03-01).
// Checks run against temp artifact-like directories and against real
// tar.gz archive fixtures, so the directory and archive access paths are
// both covered.
package laravel

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"maleolabs.com/anvil-standard-laravel/internal/contracts"
)

// writeArtifactDir creates a temp directory artifact containing the given
// relative files (empty contents) and returns its path.
func writeArtifactDir(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, rel := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

// writeArtifactContents creates a temp directory artifact containing the
// given relative files with the given contents and returns its path.
func writeArtifactContents(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

// writeArtifactStructure creates a temp directory artifact containing the
// given relative files (empty contents) and directories, and returns its
// path. Directories are created first, so files nested inside them are
// placed into the real directories.
func writeArtifactStructure(t *testing.T, files, dirs []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, rel := range dirs {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}
	for _, rel := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

// writeArtifactArchive creates a tar.gz archive containing the given
// relative files under the "app/" deployable-content prefix (the Anvil
// artifact convention) and returns its path.
func writeArtifactArchive(t *testing.T, files ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	gzr := gzip.NewWriter(f)
	tr := tar.NewWriter(gzr)
	for _, rel := range files {
		content := []byte("fixture")
		if err := tr.WriteHeader(&tar.Header{
			Name: filepath.ToSlash(filepath.Join("app", rel)),
			Mode: 0644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write header for %s: %v", rel, err)
		}
		if _, err := tr.Write(content); err != nil {
			t.Fatalf("write content for %s: %v", rel, err)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzr.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return path
}

// writeArtifactArchiveWithDirs creates a tar.gz archive containing the
// given relative files and directories under the "app/" deployable-content
// prefix and returns its path. Directories are written as tar.TypeDir
// entries with a trailing slash, as most tar writers emit them.
func writeArtifactArchiveWithDirs(t *testing.T, files, dirs []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	gzr := gzip.NewWriter(f)
	tr := tar.NewWriter(gzr)
	for _, rel := range dirs {
		if err := tr.WriteHeader(&tar.Header{
			Name:     filepath.ToSlash(filepath.Join("app", rel)) + "/",
			Mode:     0755,
			Typeflag: tar.TypeDir,
		}); err != nil {
			t.Fatalf("write dir header for %s: %v", rel, err)
		}
	}
	for _, rel := range files {
		content := []byte("fixture")
		if err := tr.WriteHeader(&tar.Header{
			Name: filepath.ToSlash(filepath.Join("app", rel)),
			Mode: 0644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write header for %s: %v", rel, err)
		}
		if _, err := tr.Write(content); err != nil {
			t.Fatalf("write content for %s: %v", rel, err)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzr.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return path
}

// TestRunVerification_Pass verifies that each check passes when the
// required files exist in the artifact directory (TS-P7-11 AC-1..AC-4,
// TS-P7-17 AC-1..AC-3).
func TestRunVerification_Pass(t *testing.T) {
	tests := []struct {
		check string
		files []string
	}{
		{check: CheckVendorPresent, files: []string{"vendor/autoload.php"}},
		{check: CheckBootstrapStructure, files: []string{"bootstrap/app.php"}},
		{check: CheckConfigFiles, files: []string{"config/app.php", ".env.example"}},
		{check: CheckArtisanFile, files: []string{"artisan"}},
		{check: CheckComposerJSON, files: []string{"composer.json"}},
		{check: CheckEnvFile, files: []string{".env"}},
	}
	for _, tt := range tests {
		t.Run(tt.check, func(t *testing.T) {
			artifactPath := writeArtifactDir(t, tt.files...)
			outcome := RunVerification(contracts.VerificationRequest{Check: tt.check, ArtifactPath: artifactPath})

			if !outcome.Passed {
				t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
			}
			if outcome.Name != tt.check {
				t.Errorf("Name = %q, want %q", outcome.Name, tt.check)
			}
			if outcome.Details == "" {
				t.Error("Details = empty, want a description of what was validated")
			}
		})
	}
}

// TestRunVerification_DirectoryPass verifies that the directory checks
// pass when the required directories exist in the artifact directory
// (TS-P7-17 AC-1..AC-3).
func TestRunVerification_DirectoryPass(t *testing.T) {
	artifactPath := writeArtifactStructure(t,
		[]string{"app/Http/Controllers/Controller.php", "routes/web.php"},
		[]string{"app", "routes"},
	)

	for _, check := range []string{CheckAppDirectory, CheckRoutesDirectory} {
		outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
		}
		if outcome.Name != check {
			t.Errorf("Name = %q, want %q", outcome.Name, check)
		}
		if outcome.Details == "" {
			t.Errorf("%s: Details = empty, want a description of what was validated", check)
		}
	}
}

// TestRunVerification_Fail verifies that each check fails with details
// naming the missing file when a required file is absent (TS-P7-11 AC-4,
// TS-P7-17 AC-2, AC-3).
func TestRunVerification_Fail(t *testing.T) {
	tests := []struct {
		check       string
		present     []string
		missingFile string
	}{
		{check: CheckVendorPresent, present: nil, missingFile: "vendor/autoload.php"},
		{check: CheckBootstrapStructure, present: []string{"vendor/autoload.php"}, missingFile: "bootstrap/app.php"},
		{check: CheckConfigFiles, present: []string{"config/app.php"}, missingFile: ".env.example"},
		{check: CheckArtisanFile, present: nil, missingFile: "artisan"},
		{check: CheckComposerJSON, present: []string{"artisan"}, missingFile: "composer.json"},
		{check: CheckEnvFile, present: []string{"artisan"}, missingFile: ".env.example"},
	}
	for _, tt := range tests {
		t.Run(tt.check, func(t *testing.T) {
			artifactPath := writeArtifactDir(t, tt.present...)
			outcome := RunVerification(contracts.VerificationRequest{Check: tt.check, ArtifactPath: artifactPath})

			if outcome.Passed {
				t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
			}
			if !strings.Contains(outcome.Details, tt.missingFile) {
				t.Errorf("Details = %q, want mention of missing file %q", outcome.Details, tt.missingFile)
			}
		})
	}
}

// TestRunVerification_DirectoryFail verifies that the directory checks
// fail with a descriptive message when the required directory is absent
// from the artifact directory (TS-P7-17 AC-2, AC-3).
func TestRunVerification_DirectoryFail(t *testing.T) {
	tests := []struct {
		check         string
		present       []string
		missingDetail string
	}{
		{check: CheckAppDirectory, present: []string{"routes/web.php"}, missingDetail: "missing required directory: app"},
		{check: CheckRoutesDirectory, present: []string{"app/Http/Controllers/Controller.php"}, missingDetail: "missing required directory: routes"},
	}
	for _, tt := range tests {
		t.Run(tt.check, func(t *testing.T) {
			artifactPath := writeArtifactDir(t, tt.present...)
			outcome := RunVerification(contracts.VerificationRequest{Check: tt.check, ArtifactPath: artifactPath})

			if outcome.Passed {
				t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
			}
			if !strings.Contains(outcome.Details, tt.missingDetail) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, tt.missingDetail)
			}
		})
	}
}

// TestRunVerification_EnvFile verifies the env_file alternatives: the
// check passes when either .env or .env.example is present and fails
// with a descriptive message when neither is (TS-P7-17 AC-1..AC-3).
func TestRunVerification_EnvFile(t *testing.T) {
	t.Run("env_only", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckEnvFile,
			ArtifactPath: writeArtifactDir(t, ".env"),
		})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
	})

	t.Run("env_example_only", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckEnvFile,
			ArtifactPath: writeArtifactDir(t, ".env.example"),
		})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
	})

	t.Run("neither", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckEnvFile,
			ArtifactPath: writeArtifactDir(t, "artisan", "composer.json"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		if !strings.Contains(outcome.Details, "neither .env nor .env.example found") {
			t.Errorf("Details = %q, want the descriptive missing message", outcome.Details)
		}
	})
}

// TestRunVerification_AppDirectory_FileNotDirectory verifies that a
// regular file named "app" does not satisfy the app_directory check —
// only a real directory counts (TS-P7-17 AC-2).
func TestRunVerification_AppDirectory_FileNotDirectory(t *testing.T) {
	t.Run("directory_artifact", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckAppDirectory,
			ArtifactPath: writeArtifactDir(t, "app"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
	})

	t.Run("archive", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckAppDirectory,
			ArtifactPath: writeArtifactArchive(t, "app"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
	})
}

// TestRunVerification_Archive verifies that checks also pass against a
// real tar.gz artifact archive with the "app/" deployable-content prefix,
// and fail when the entry is absent — no full extraction is performed.
func TestRunVerification_Archive(t *testing.T) {
	t.Run("pass_existing", func(t *testing.T) {
		artifactPath := writeArtifactArchive(t, "vendor/autoload.php", "bootstrap/app.php", "config/app.php", ".env.example")
		for _, check := range []string{CheckVendorPresent, CheckBootstrapStructure, CheckConfigFiles} {
			outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
			}
		}
	})

	t.Run("pass_new_checks", func(t *testing.T) {
		// The archive contains the required files and explicit TypeDir
		// entries for app/ and routes/, as most tar writers emit them.
		artifactPath := writeArtifactArchiveWithDirs(t,
			[]string{
				"vendor/autoload.php",
				"bootstrap/app.php",
				"config/app.php",
				".env.example",
				"artisan",
				"composer.json",
				"routes/web.php",
				"app/Http/Controllers/Controller.php",
			},
			[]string{"app", "routes"},
		)
		for _, check := range []string{
			CheckVendorPresent,
			CheckBootstrapStructure,
			CheckConfigFiles,
			CheckArtisanFile,
			CheckComposerJSON,
			CheckEnvFile,
			CheckAppDirectory,
			CheckRoutesDirectory,
		} {
			outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
			}
		}
	})

	t.Run("pass_new_checks_no_dir_entries", func(t *testing.T) {
		// Anvil packaging stores only regular files, never directory
		// entries: a directory still counts when entries live beneath it.
		artifactPath := writeArtifactArchive(t,
			"artisan", "composer.json", ".env.example",
			"routes/web.php", "app/Http/Controllers/Controller.php",
		)
		for _, check := range []string{CheckAppDirectory, CheckRoutesDirectory} {
			outcome := RunVerification(contracts.VerificationRequest{Check: check, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("%s: Passed = false, want true (outcome: %#v)", check, outcome)
			}
		}
	})

	t.Run("env_alternatives", func(t *testing.T) {
		for _, files := range [][]string{{".env"}, {".env.example"}} {
			artifactPath := writeArtifactArchive(t, files...)
			outcome := RunVerification(contracts.VerificationRequest{Check: CheckEnvFile, ArtifactPath: artifactPath})
			if !outcome.Passed {
				t.Errorf("files %v: Passed = false, want true (outcome: %#v)", files, outcome)
			}
		}
	})

	t.Run("fail_existing", func(t *testing.T) {
		artifactPath := writeArtifactArchive(t, "bootstrap/app.php")
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckVendorPresent, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Error("Passed = true, want false")
		}
		if !strings.Contains(outcome.Details, "vendor/autoload.php") {
			t.Errorf("Details = %q, want mention of vendor/autoload.php", outcome.Details)
		}
	})

	t.Run("fail_new_checks", func(t *testing.T) {
		// The archive has none of the entries the new checks require:
		// no artisan/composer.json/env files, and neither directory.
		artifactPath := writeArtifactArchive(t, "bootstrap/app.php", "config/app.php")
		tests := []struct {
			check         string
			missingDetail string
		}{
			{check: CheckArtisanFile, missingDetail: "artisan"},
			{check: CheckComposerJSON, missingDetail: "composer.json"},
			{check: CheckEnvFile, missingDetail: "neither .env nor .env.example found"},
			{check: CheckAppDirectory, missingDetail: "missing required directory: app"},
			{check: CheckRoutesDirectory, missingDetail: "missing required directory: routes"},
		}
		for _, tt := range tests {
			outcome := RunVerification(contracts.VerificationRequest{Check: tt.check, ArtifactPath: artifactPath})
			if outcome.Passed {
				t.Errorf("%s: Passed = true, want false (outcome: %#v)", tt.check, outcome)
			}
			if !strings.Contains(outcome.Details, tt.missingDetail) {
				t.Errorf("%s: Details = %q, want it to contain %q", tt.check, outcome.Details, tt.missingDetail)
			}
		}
	})
}

// writeArtifactArchiveContents creates a tar.gz archive containing the
// given relative files with the given contents under the "app/"
// deployable-content prefix and returns its path.
func writeArtifactArchiveContents(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "artifact.tar.gz")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	gzr := gzip.NewWriter(f)
	tr := tar.NewWriter(gzr)
	for rel, content := range files {
		if err := tr.WriteHeader(&tar.Header{
			Name: filepath.ToSlash(filepath.Join("app", rel)),
			Mode: 0644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write header for %s: %v", rel, err)
		}
		if _, err := tr.Write([]byte(content)); err != nil {
			t.Fatalf("write content for %s: %v", rel, err)
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzr.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return path
}

// cacheConfigFixture is a realistic Laravel 11-style config/cache.php:
// a default written as env('CACHE_STORE', 'database') and a stores map
// with the shipped drivers, including a driver (redis) with nested
// arrays.
const cacheConfigFixture = `<?php

return [

    'default' => env('CACHE_STORE', 'database'),

    'stores' => [

        'apc' => [
            'driver' => 'apc',
        ],

        'array' => [
            'driver' => 'array',
            'serialize' => false,
        ],

        'database' => [
            'driver' => 'database',
            'table' => 'cache',
            'connection' => env('DB_CACHE_CONNECTION'),
        ],

        'file' => [
            'driver' => 'file',
            'path' => storage_path('framework/cache/data'),
        ],

        'memcached' => [
            'driver' => 'memcached',
            'persistent_id' => env('MEMCACHED_PERSISTENT_ID'),
        ],

        'redis' => [
            'driver' => 'redis',
            'connection' => env('REDIS_CACHE_CONNECTION', 'cache'),
            'lock_connection' => env('REDIS_CACHE_LOCK_CONNECTION', 'default'),
        ],

        'dynamodb' => [
            'driver' => 'dynamodb',
            'key' => env('AWS_ACCESS_KEY_ID'),
        ],

    ],

    'prefix' => env('CACHE_PREFIX', Str::slug(env('APP_NAME', 'laravel'), '_').'_cache_'),

];
`

// compiledConfigFixture is the compiled config cache Laravel writes on
// `php artisan config:cache`: resolved literals, every config file's
// keys nested under its own section. The database section carries its
// own top-level 'default' key ('mysql') and appears before the cache
// section — the cache default extraction must read the cache section
// only and never shadow it with database's.
const compiledConfigFixture = `<?php return array (
  'app' => array (
    'name' => 'my-app',
  ),
  'database' => array (
    'default' => 'mysql',
    'connections' => array (
      'mysql' => array (
        'driver' => 'mysql',
      ),
    ),
  ),
  'cache' => array (
    'default' => 'file',
    'stores' => array (
      'file' => array (
        'driver' => 'file',
      ),
    ),
  ),
);`

// TestRunVerification_SharedResourceWiring verifies the shared-resource
// wiring check (TS-018-03-01): the declared cache store must be the
// store the release runs with, and the runtime store must be wired in
// config/cache.php.
func TestRunVerification_SharedResourceWiring(t *testing.T) {
	t.Run("pass_env_matches_compiled", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			".env":                       "APP_ENV=production\nCACHE_STORE=file\n",
			"config/cache.php":           cacheConfigFixture,
			"bootstrap/cache/config.php": compiledConfigFixture,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckSharedResourceWiring, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
		for _, want := range []string{`"file"`, "wired", "config/cache.php stores"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("pass_literal_default_without_env", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			"config/cache.php": `<?php return ['default' => 'database', 'stores' => ['database' => ['driver' => 'database']]];`,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckSharedResourceWiring, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
		if !strings.Contains(outcome.Details, "database") {
			t.Errorf("Details = %q, want it to contain the runtime store", outcome.Details)
		}
	})

	t.Run("fail_declared_store_drifts_from_compiled", func(t *testing.T) {
		// .env declares redis, the compiled config cache — what the
		// release actually runs with after config:cache — says file.
		artifactPath := writeArtifactContents(t, map[string]string{
			".env":                       "CACHE_STORE=redis\n",
			"config/cache.php":           cacheConfigFixture,
			"bootstrap/cache/config.php": compiledConfigFixture,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckSharedResourceWiring, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{`"redis"`, `"file"`, "not wired as declared"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_store_not_wired_in_stores_map", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			".env":             "CACHE_STORE=redis\n",
			"config/cache.php": `<?php return ['default' => env('CACHE_STORE', 'database'), 'stores' => ['database' => ['driver' => 'database']]];`,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckSharedResourceWiring, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"redis", "not wired for the release"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_unknown_store", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			".env":             "CACHE_STORE=banana\n",
			"config/cache.php": cacheConfigFixture,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckSharedResourceWiring, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"banana", "not a known Laravel cache store"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_missing_config_cache_php", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			".env": "CACHE_STORE=file\n",
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckSharedResourceWiring, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		if !strings.Contains(outcome.Details, "config/cache.php") {
			t.Errorf("Details = %q, want it to name config/cache.php", outcome.Details)
		}
	})

	t.Run("archive_pass", func(t *testing.T) {
		artifactPath := writeArtifactArchiveContents(t, map[string]string{
			".env":                       "CACHE_STORE=file\n",
			"config/cache.php":           cacheConfigFixture,
			"bootstrap/cache/config.php": compiledConfigFixture,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckSharedResourceWiring, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
	})

	t.Run("archive_fail_drift", func(t *testing.T) {
		artifactPath := writeArtifactArchiveContents(t, map[string]string{
			".env":                       "CACHE_STORE=redis\n",
			"config/cache.php":           cacheConfigFixture,
			"bootstrap/cache/config.php": compiledConfigFixture,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckSharedResourceWiring, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
	})
}

// TestRunVerification_MigrationTiming verifies the migration-timing
// check (TS-018-03-01): the release must carry the post-activation
// state (compiled config cache — the config:cache phase runs after the
// migrate phase) and the migration set at the declared migrations path.
func TestRunVerification_MigrationTiming(t *testing.T) {
	t.Run("pass_with_migrations", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			"bootstrap/cache/config.php":                                   compiledConfigFixture,
			"database/migrations/2024_01_01_000000_create_users_table.php": "<?php // migration",
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckMigrationTiming, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
		for _, want := range []string{"post-promotion", "database/migrations", "1 migration file"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("pass_without_migration_files", func(t *testing.T) {
		artifactPath := writeArtifactStructure(t,
			[]string{"bootstrap/cache/config.php"},
			[]string{"database/migrations"},
		)
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckMigrationTiming, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
		if !strings.Contains(outcome.Details, "0 migration file") {
			t.Errorf("Details = %q, want it to report zero migration files", outcome.Details)
		}
	})

	t.Run("fail_no_post_activation_state", func(t *testing.T) {
		artifactPath := writeArtifactDir(t, "database/migrations/2024_01_01_000000_create_users_table.php")
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckMigrationTiming, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"bootstrap/cache/config.php", "no post-activation state"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_migrations_path_missing", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			"bootstrap/cache/config.php": compiledConfigFixture,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckMigrationTiming, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"database/migrations", "stripped from the release"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_non_migration_files", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			"bootstrap/cache/config.php":    compiledConfigFixture,
			"database/migrations/notes.txt": "not a migration",
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckMigrationTiming, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"notes.txt", "not well-formed"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("archive_pass", func(t *testing.T) {
		artifactPath := writeArtifactArchiveContents(t, map[string]string{
			"bootstrap/cache/config.php":                                   compiledConfigFixture,
			"database/migrations/2024_01_01_000000_create_users_table.php": "<?php // migration",
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckMigrationTiming, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
	})

	t.Run("archive_fail", func(t *testing.T) {
		artifactPath := writeArtifactArchiveContents(t, map[string]string{
			"database/migrations/2024_01_01_000000_create_users_table.php": "<?php // migration",
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckMigrationTiming, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
	})
}

// TestQueueRestartSignalPath pins the file-cache path of the queue
// restart signal: Laravel's FileStore::path derivation (sha1 of the key,
// two two-character directory levels, file named by the full hash) under
// storage/framework/cache/data. The pinned value is the re-checkable
// evidence location of the queue_restart check.
func TestQueueRestartSignalPath(t *testing.T) {
	want := "storage/framework/cache/data/68/05/680501494cbbcedd56c897bac4b527faa882d3a5"
	if got := queueRestartSignalPath(); got != want {
		t.Errorf("queueRestartSignalPath() = %q, want %q", got, want)
	}
}

// TestRunVerification_QueueRestart verifies the queue-restart check
// (TS-018-03-01): with the file cache store the restart signal must be
// present in the release's file cache store; with any other store the
// evidence location is the shared store, declared in the details.
func TestRunVerification_QueueRestart(t *testing.T) {
	t.Run("pass_file_store_signal_present", func(t *testing.T) {
		signalPath := queueRestartSignalPath()
		artifactPath := writeArtifactContents(t, map[string]string{
			".env":             "CACHE_STORE=file\n",
			"config/cache.php": cacheConfigFixture,
			signalPath:         "1723000000",
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckQueueRestart, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
		for _, want := range []string{"queue restart signal present", signalPath} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_file_store_signal_missing", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			".env":             "CACHE_STORE=file\n",
			"config/cache.php": cacheConfigFixture,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckQueueRestart, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"no queue restart signal", queueRestartSignalPath()} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("pass_non_file_store_declares_external_evidence", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			".env":             "CACHE_STORE=redis\n",
			"config/cache.php": cacheConfigFixture,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckQueueRestart, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
		for _, want := range []string{"redis", "laravel_database_queues_restart", "external to the release directory"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_store_undeterminable", func(t *testing.T) {
		artifactPath := writeArtifactContents(t, map[string]string{
			"config/cache.php": `<?php return ['stores' => ['file' => ['driver' => 'file']]];`,
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckQueueRestart, ArtifactPath: artifactPath})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"cannot determine the cache store", "no CACHE_STORE", "no default"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("archive_pass_file_store", func(t *testing.T) {
		signalPath := queueRestartSignalPath()
		artifactPath := writeArtifactArchiveContents(t, map[string]string{
			".env":             "CACHE_STORE=file\n",
			"config/cache.php": cacheConfigFixture,
			signalPath:         "1723000000",
		})
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckQueueRestart, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
	})
}

// TestRunVerification_RollbackBehavior verifies the rollback-behavior
// check (TS-018-03-01): rollback produces the declared state — every
// phase declares rollback coverage, the migration rollback is the
// force-confirmed `migrate:rollback --force`, and the manifest rollback
// metadata matches the phase table.
func TestRunVerification_RollbackBehavior(t *testing.T) {
	t.Run("pass_declared_semantics", func(t *testing.T) {
		artifactPath := writeArtifactDir(t, "vendor/autoload.php")
		outcome := RunVerification(contracts.VerificationRequest{Check: CheckRollbackBehavior, ArtifactPath: artifactPath})
		if !outcome.Passed {
			t.Errorf("Passed = false, want true (outcome: %#v)", outcome)
		}
		for _, want := range []string{
			"rollback produces the declared state",
			"migrate: php artisan migrate:rollback --force",
			"informational (irreversible, rollback never blocks)",
			"manifest rollback metadata matches",
		} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_missing_artifact", func(t *testing.T) {
		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckRollbackBehavior,
			ArtifactPath: filepath.Join(t.TempDir(), "does-not-exist"),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		if !strings.Contains(outcome.Details, "cannot inspect artifact") {
			t.Errorf("Details = %q, want it to mention the inspection failure", outcome.Details)
		}
	})

	t.Run("fail_irreversible_phase_with_command", func(t *testing.T) {
		original := phases
		phases = []phase{
			{name: "migrate", activateArgs: []string{"migrate", "--force"}, rollbackArgs: []string{"migrate:rollback", "--force"}},
			{name: "config_cache", activateArgs: []string{"config:cache"}, irreversible: true, rollbackArgs: []string{"config:clear"}},
		}
		defer func() { phases = original }()

		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckRollbackBehavior,
			ArtifactPath: t.TempDir(),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"config_cache", "irreversible", "rollback command"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_reversible_phase_without_command", func(t *testing.T) {
		original := phases
		phases = []phase{
			{name: "migrate", activateArgs: []string{"migrate", "--force"}},
		}
		defer func() { phases = original }()

		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckRollbackBehavior,
			ArtifactPath: t.TempDir(),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"migrate", "rollback coverage is missing"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})

	t.Run("fail_migrate_rollback_not_force_confirmed", func(t *testing.T) {
		original := phases
		phases = []phase{
			{name: "migrate", activateArgs: []string{"migrate", "--force"}, rollbackArgs: []string{"migrate:rollback"}},
		}
		defer func() { phases = original }()

		outcome := RunVerification(contracts.VerificationRequest{
			Check:        CheckRollbackBehavior,
			ArtifactPath: t.TempDir(),
		})
		if outcome.Passed {
			t.Errorf("Passed = true, want false (outcome: %#v)", outcome)
		}
		for _, want := range []string{"migrate:rollback --force", "force-confirmed"} {
			if !strings.Contains(outcome.Details, want) {
				t.Errorf("Details = %q, want it to contain %q", outcome.Details, want)
			}
		}
	})
}

// TestRunVerification_UnknownCheck verifies that an undeclared check
// yields a failing outcome with details, not a panic.
func TestRunVerification_UnknownCheck(t *testing.T) {
	outcome := RunVerification(contracts.VerificationRequest{Check: "unknown_check", ArtifactPath: t.TempDir()})
	if outcome.Passed {
		t.Error("Passed = true, want false")
	}
	if !strings.Contains(outcome.Details, `unknown verification check "unknown_check"`) {
		t.Errorf("Details = %q, want mention of the unknown check", outcome.Details)
	}
}

// TestRunVerification_MissingArtifact verifies that an unreadable artifact
// path yields a failing outcome with an actionable detail.
func TestRunVerification_MissingArtifact(t *testing.T) {
	outcome := RunVerification(contracts.VerificationRequest{
		Check:        CheckVendorPresent,
		ArtifactPath: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if outcome.Passed {
		t.Error("Passed = true, want false")
	}
	if !strings.Contains(outcome.Details, "cannot inspect artifact") {
		t.Errorf("Details = %q, want mention of the inspection failure", outcome.Details)
	}
}

// TestCapabilities_DeclaresChecks verifies that the capability declaration
// lists exactly the twelve verification checks — the eight structural
// ones (the three original TS-P7-11 plus the five additive ones TS-P7-17)
// and the four lifecycle-conformity ones (TS-018-03-01) — in declaration
// order (TS-P7-11 DoD, TS-P7-17 DoD, TS-018-03-01 DoD).
func TestCapabilities_DeclaresChecks(t *testing.T) {
	result := Capabilities()
	checks := result.Declaration.VerificationChecks
	if len(checks) != 12 {
		t.Fatalf("VerificationChecks length = %d, want 12", len(checks))
	}
	want := []string{
		CheckVendorPresent,
		CheckBootstrapStructure,
		CheckConfigFiles,
		CheckArtisanFile,
		CheckComposerJSON,
		CheckEnvFile,
		CheckAppDirectory,
		CheckRoutesDirectory,
		CheckSharedResourceWiring,
		CheckMigrationTiming,
		CheckQueueRestart,
		CheckRollbackBehavior,
	}
	for i, name := range want {
		if checks[i].Name != name {
			t.Errorf("VerificationChecks[%d].Name = %q, want %q", i, checks[i].Name, name)
		}
		if checks[i].Description == "" {
			t.Errorf("VerificationChecks[%d].Description = empty, want a description", i)
		}
	}
}
