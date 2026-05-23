package upgrade

// RequiredSchemaVersion is the schema migration version this binary requires.
// Bump this whenever adding a new SQL migration file.
//
// Fork-only: kept at the highest 099xxx fork-only migration number so the
// upgrade runner picks up every fork-only migration in /app/migrations.
// Without this bump CheckSchema marks current == required as "up to date"
// even when migrations files exist on disk — golang-migrate is never asked
// to scan them. On every upstream merge that raises this constant, max
// it with our current fork-only 099xxx top.
const RequiredSchemaVersion uint = 99001
