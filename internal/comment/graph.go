package comment

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

const schema = `
CREATE TABLE IF NOT EXISTS comment_nodes (
	id              TEXT PRIMARY KEY,
	file            TEXT NOT NULL,
	line            INTEGER NOT NULL,
	end_line        INTEGER NOT NULL,
	content         TEXT NOT NULL,
	kind            TEXT NOT NULL,
	associated_decl TEXT DEFAULT '',
	first_seen      DATETIME DEFAULT CURRENT_TIMESTAMP,
	last_code_change DATETIME,
	staleness       REAL DEFAULT 0,
	tags_json       TEXT DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS comment_refs (
	source_id   TEXT NOT NULL REFERENCES comment_nodes(id),
	target_type TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	resolved    TEXT DEFAULT '',
	status      TEXT DEFAULT 'unknown',
	last_checked DATETIME,
	UNIQUE(source_id, target_type, target_id)
);

CREATE INDEX IF NOT EXISTS idx_comment_file ON comment_nodes(file);
CREATE INDEX IF NOT EXISTS idx_comment_kind ON comment_nodes(kind);
CREATE INDEX IF NOT EXISTS idx_ref_status ON comment_refs(status);
CREATE INDEX IF NOT EXISTS idx_ref_source ON comment_refs(source_id);
`

// Graph stores comment nodes and references in SQLite.
type Graph struct {
	db   *sql.DB
	path string
}

// OpenGraph opens or creates the comment graph database.
// If dbPath is empty, uses .beads/comments.db in the current directory.
func OpenGraph(dbPath string) (*Graph, error) {
	if dbPath == "" {
		dbPath = filepath.Join(".beads", "comments.db")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &Graph{db: db, path: dbPath}, nil
}

// Close closes the database connection.
func (g *Graph) Close() error {
	return g.db.Close()
}

// StoreScanResult persists a full scan result, replacing previous data.
func (g *Graph) StoreScanResult(result *ScanResult) error {
	tx, err := g.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear previous data.
	if _, err := tx.Exec("DELETE FROM comment_refs"); err != nil {
		return fmt.Errorf("clearing refs: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM comment_nodes"); err != nil {
		return fmt.Errorf("clearing nodes: %w", err)
	}

	nodeStmt, err := tx.Prepare(`
		INSERT INTO comment_nodes (id, file, line, end_line, content, kind, associated_decl, first_seen, staleness, tags_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing node insert: %w", err)
	}
	defer nodeStmt.Close()

	refStmt, err := tx.Prepare(`
		INSERT INTO comment_refs (source_id, target_type, target_id, resolved, status, last_checked)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing ref insert: %w", err)
	}
	defer refStmt.Close()

	now := time.Now()
	for _, node := range result.Nodes {
		tagsJSON, _ := json.Marshal(node.Tags)
		if _, err := nodeStmt.Exec(
			node.ID, node.File, node.Line, node.EndLine,
			node.Content, string(node.Kind), node.AssociatedDecl,
			now, node.Staleness, string(tagsJSON),
		); err != nil {
			// Duplicate IDs possible if same content in different locations; skip.
			continue
		}

		for _, ref := range node.References {
			if _, err := refStmt.Exec(
				node.ID, string(ref.TargetType), ref.Target,
				ref.Resolved, string(ref.Status), now,
			); err != nil {
				continue
			}
		}
	}

	return tx.Commit()
}

// GetAllNodes retrieves all stored comment nodes.
func (g *Graph) GetAllNodes() ([]Node, error) {
	rows, err := g.db.Query(`
		SELECT id, file, line, end_line, content, kind, associated_decl, first_seen, staleness, tags_json
		FROM comment_nodes ORDER BY file, line
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var kindStr, tagsJSON string
		var firstSeen time.Time
		if err := rows.Scan(&n.ID, &n.File, &n.Line, &n.EndLine, &n.Content,
			&kindStr, &n.AssociatedDecl, &firstSeen, &n.Staleness, &tagsJSON); err != nil {
			return nil, err
		}
		n.Kind = Kind(kindStr)
		n.FirstSeen = firstSeen
		_ = json.Unmarshal([]byte(tagsJSON), &n.Tags)

		// Load references for this node.
		refs, err := g.getRefsForNode(n.ID)
		if err != nil {
			return nil, err
		}
		n.References = refs
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// GetNodesByFile retrieves comment nodes for a specific file.
func (g *Graph) GetNodesByFile(file string) ([]Node, error) {
	rows, err := g.db.Query(`
		SELECT id, file, line, end_line, content, kind, associated_decl, first_seen, staleness, tags_json
		FROM comment_nodes WHERE file = ? ORDER BY line
	`, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var kindStr, tagsJSON string
		var firstSeen time.Time
		if err := rows.Scan(&n.ID, &n.File, &n.Line, &n.EndLine, &n.Content,
			&kindStr, &n.AssociatedDecl, &firstSeen, &n.Staleness, &tagsJSON); err != nil {
			return nil, err
		}
		n.Kind = Kind(kindStr)
		n.FirstSeen = firstSeen
		_ = json.Unmarshal([]byte(tagsJSON), &n.Tags)

		refs, err := g.getRefsForNode(n.ID)
		if err != nil {
			return nil, err
		}
		n.References = refs
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// GetBrokenRefs returns all references with status "broken".
func (g *Graph) GetBrokenRefs() ([]Node, error) {
	rows, err := g.db.Query(`
		SELECT DISTINCT n.id, n.file, n.line, n.end_line, n.content, n.kind, n.associated_decl, n.first_seen, n.staleness, n.tags_json
		FROM comment_nodes n
		JOIN comment_refs r ON r.source_id = n.id
		WHERE r.status = 'broken'
		ORDER BY n.file, n.line
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var kindStr, tagsJSON string
		var firstSeen time.Time
		if err := rows.Scan(&n.ID, &n.File, &n.Line, &n.EndLine, &n.Content,
			&kindStr, &n.AssociatedDecl, &firstSeen, &n.Staleness, &tagsJSON); err != nil {
			return nil, err
		}
		n.Kind = Kind(kindStr)
		n.FirstSeen = firstSeen
		_ = json.Unmarshal([]byte(tagsJSON), &n.Tags)

		refs, err := g.getRefsForNode(n.ID)
		if err != nil {
			return nil, err
		}
		n.References = refs
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// UpdateRefStatus updates the status of a specific reference.
func (g *Graph) UpdateRefStatus(sourceID string, targetType RefTargetType, target string, status RefStatus, resolved string) error {
	_, err := g.db.Exec(`
		UPDATE comment_refs SET status = ?, resolved = ?, last_checked = ?
		WHERE source_id = ? AND target_type = ? AND target_id = ?
	`, string(status), resolved, time.Now(), sourceID, string(targetType), target)
	return err
}

// Stats returns summary statistics for the comment graph.
func (g *Graph) Stats() (map[string]int, error) {
	stats := make(map[string]int)

	var total int
	if err := g.db.QueryRow("SELECT COUNT(*) FROM comment_nodes").Scan(&total); err != nil {
		return nil, err
	}
	stats["total"] = total

	rows, err := g.db.Query("SELECT kind, COUNT(*) FROM comment_nodes GROUP BY kind")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		stats[kind] = count
	}

	var broken int
	if err := g.db.QueryRow("SELECT COUNT(*) FROM comment_refs WHERE status = 'broken'").Scan(&broken); err != nil {
		return nil, err
	}
	stats["broken_refs"] = broken

	var totalRefs int
	if err := g.db.QueryRow("SELECT COUNT(*) FROM comment_refs").Scan(&totalRefs); err != nil {
		return nil, err
	}
	stats["total_refs"] = totalRefs

	return stats, nil
}

func (g *Graph) getRefsForNode(nodeID string) ([]Ref, error) {
	rows, err := g.db.Query(`
		SELECT target_type, target_id, resolved, status
		FROM comment_refs WHERE source_id = ?
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Ref
	for rows.Next() {
		var r Ref
		var tt, st string
		if err := rows.Scan(&tt, &r.Target, &r.Resolved, &st); err != nil {
			return nil, err
		}
		r.TargetType = RefTargetType(tt)
		r.Status = RefStatus(st)
		refs = append(refs, r)
	}
	return refs, rows.Err()
}
