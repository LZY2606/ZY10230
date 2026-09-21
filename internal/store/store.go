package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const SchemaVersion = 3

type DB struct {
	Sql *sql.DB
}

func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if _, err := sqlDB.Exec(`INSERT OR IGNORE INTO meta(key,value) VALUES('schema_version',?)`, SchemaVersion); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return &DB{Sql: sqlDB}, nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sections(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  ord INTEGER NOT NULL,
  depth_unit TEXT NOT NULL DEFAULT 'm'
);

CREATE TABLE IF NOT EXISTS taxa(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sample_intervals(
  id TEXT PRIMARY KEY,
  section_id TEXT NOT NULL REFERENCES sections(id),
  top_depth REAL NOT NULL,
  base_depth REAL NOT NULL,
  sufficient INTEGER NOT NULL,
  note TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS occurrences(
  id TEXT PRIMARY KEY,
  section_id TEXT NOT NULL REFERENCES sections(id),
  taxon_id TEXT NOT NULL REFERENCES taxa(id),
  depth REAL NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('present','absent')),
  interval_id TEXT REFERENCES sample_intervals(id),
  note TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS syn_versions(
  version INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  kind TEXT NOT NULL CHECK(kind IN ('merge','split','create')),
  group_id TEXT NOT NULL,
  display_name TEXT NOT NULL,
  members TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS syn_groups(
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS syn_members(
  group_id TEXT NOT NULL REFERENCES syn_groups(id),
  taxon_id TEXT NOT NULL REFERENCES taxa(id),
  PRIMARY KEY(group_id, taxon_id)
);

CREATE TABLE IF NOT EXISTS rework_candidates(
  occurrence_id TEXT PRIMARY KEY REFERENCES occurrences(id),
  marked INTEGER NOT NULL,
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  note TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ties(
  id TEXT PRIMARY KEY,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  a_group TEXT NOT NULL,
  a_section TEXT NOT NULL,
  a_kind TEXT NOT NULL CHECK(a_kind IN ('FAD','LAD')),
  b_group TEXT NOT NULL,
  b_section TEXT NOT NULL,
  b_kind TEXT NOT NULL CHECK(b_kind IN ('FAD','LAD')),
  locked INTEGER NOT NULL DEFAULT 1,
  active INTEGER NOT NULL DEFAULT 1,
  conflicted INTEGER NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS events(
  id TEXT PRIMARY KEY,
  revision INTEGER NOT NULL DEFAULT 1,
  interpretation TEXT NOT NULL CHECK(interpretation IN ('raw','in_situ')),
  kind TEXT NOT NULL CHECK(kind IN ('FAD','LAD','COEX')),
  group_id TEXT NOT NULL,
  section_id TEXT NOT NULL,
  depth REAL,
  partner_group TEXT,
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE(interpretation,kind,group_id,section_id,partner_group)
);

CREATE TABLE IF NOT EXISTS event_evidence(
  event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  occurrence_id TEXT REFERENCES occurrences(id),
  interval_id TEXT REFERENCES sample_intervals(id),
  role TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(event_id,role,occurrence_id,interval_id)
);

CREATE TABLE IF NOT EXISTS recompute_log(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL DEFAULT (datetime('now')),
  cause TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT '',
  recomputed INTEGER NOT NULL,
  preserved INTEGER NOT NULL,
  detail TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS runs(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL DEFAULT (datetime('now')),
  kind TEXT NOT NULL,
  payload TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_occ_lookup ON occurrences(section_id,taxon_id);
CREATE INDEX IF NOT EXISTS idx_intervals ON sample_intervals(section_id);
`
