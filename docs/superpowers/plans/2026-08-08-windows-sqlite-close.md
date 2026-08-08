# Windows SQLite Test Handle Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every SQLite database handle opened by the persistence test before `t.TempDir` removes its database file on Windows.

**Architecture:** The test already closes its first GORM connection before reopening the database. It will retrieve and register cleanup for the second underlying `*sql.DB` as soon as the reopened GORM connection succeeds; Go runs this cleanup before the earlier `t.TempDir` cleanup. No production database behavior changes.

**Tech Stack:** Go 1.20, GORM, pure-Go SQLite driver, Go `testing` package.

## Global Constraints

- The test must keep validating migration and persistence through a reopened SQLite connection.
- All database handles must close before the temporary directory cleanup, including when a later assertion fails.
- The verification must compile for Windows; Linux cannot reproduce Windows mandatory file locking.

---

### Task 1: Close the reopened SQLite handle

**Files:**
- Modify: `internal/pkg/database_test.go`
- Create: `docs/superpowers/plans/2026-08-08-windows-sqlite-close.md`

**Interfaces:**
- Consumes: `reopened *gorm.DB` from `gorm.Open`.
- Produces: a registered `reopenedSQLDB.Close()` cleanup that runs before `t.TempDir` removes `cameraio.db`.

- [x] **Step 1: Confirm the failing regression case**

The Windows CI run fails in `TestPureGoSQLiteDriverMigratesAndPersists` during `TempDir RemoveAll cleanup` because `cameraio.db` is still open after the persistence query.

- [x] **Step 2: Add cleanup immediately after reopening**

```go
reopenedSQLDB, err := reopened.DB()
if err != nil {
	t.Fatalf("get reopened database handle: %v", err)
}
defer func() {
	if err := reopenedSQLDB.Close(); err != nil {
		t.Errorf("close reopened database: %v", err)
	}
}()
```

- [x] **Step 3: Run package tests**

Run: `CGO_ENABLED=0 go test ./internal/pkg -count=1`

Expected: PASS. The original Windows failure is platform-specific because Unix permits unlinking an open file.

- [x] **Step 4: Compile the test for Windows**

Run: `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test -c -o /tmp/cameraio-database-windows.test.exe ./internal/pkg`

Expected: PASS and emits a Windows PE test binary.

- [x] **Step 5: Run the full suite**

Run: `CGO_ENABLED=0 CAMERAIO_SKIP_FFMPEG_DOWNLOAD=1 go test ./... -count=1`

Expected: PASS.
