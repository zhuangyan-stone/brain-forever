// Command backfill-excerpt-vectors generates missing embedding vectors for all
// existing excerpts that lack a corresponding row in the excerpt_vectors table.
//
// This is a one-shot migration tool. It is safe (idempotent) to run multiple times.
//
// All configuration is read from bin/settings/server.toml (same as the main server).
// No extra flags or env vars needed — just run it:
//
//	cd brain-forever && go run cmd/backfill-excerpt-vectors/main.go
//
// The tool reads:
//   - Database DSN: from PG_DSN env var (same convention as main server)
//   - Embedder provider: from [api-keys].default_embedding_provider ("ali" or "zhipu")
//   - Embedder API key: from [api-keys]."embedding@<provider>"
//
// Optional flags (for testing):
//
//	--batch-size   Number of excerpts to process per batch (default: 50)
//	--limit        Max total excerpts to process; 0 = unlimited (default: 0)
//	--dry-run      Only count and show what would be done, without generating vectors
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"BrainForever/infra/embedder"
	"BrainForever/internal/config"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/pgvector/pgvector-go"
)

// ============================================================
// CLI flags
// ============================================================

type programFlags struct {
	batchSize int
	limit     int
	dryRun    bool
	provider  string // override embedder provider from config
}

func parseFlags() programFlags {
	var f programFlags
	flag.IntVar(&f.batchSize, "batch-size", 50, "Number of excerpts to process per batch")
	flag.IntVar(&f.limit, "limit", 0, "Max total excerpts to process; 0 = unlimited")
	flag.BoolVar(&f.dryRun, "dry-run", false, "Only count and show what would be done, without generating vectors")
	flag.StringVar(&f.provider, "provider", "", "Override embedder provider (ali or zhipu). Default: read from config")
	flag.Parse()
	return f
}

// ============================================================
// excerptRow — minimal fields needed for vector generation
// ============================================================

type excerptRow struct {
	ID             int64  `db:"id"`
	Content        string `db:"content"`
	ContextSummary string `db:"context_summary"`
}

// ============================================================
// entry point
// ============================================================

func main() {
	f := parseFlags()

	// ----------------------------------------------------------
	// Load server config (bin/settings/server.toml)
	// ----------------------------------------------------------
	cfg := config.DefaultConfig()
	if err := cfg.LoadFromFile("bin/settings/server.toml"); err != nil {
		log.Fatalf("failed to load bin/settings/server.toml. %v", err)
	}
	config.InitApiKeysPool(cfg.ApiKeys)
	pool := config.GetApiKeysPool()

	// ----------------------------------------------------------
	// Resolve embedder provider: --provider flag > config > default
	// ----------------------------------------------------------
	embedderProvider := f.provider
	if embedderProvider == "" {
		embedderProvider = pool.DefaultEmbeddingProvider
		if embedderProvider == "" {
			embedderProvider = "ali" // fallback
		}
	}
	// Resolve API key for the chosen provider.
	embedderAPIKey := pool.GetOne("embedding", embedderProvider)
	if embedderAPIKey == "" {
		log.Fatalf("no API key found for embedding provider %q. "+
			"Check [api-keys] section in bin/settings/server.toml "+
			"for an \"embedding@%s\" entry", embedderProvider, embedderProvider)
	}
	log.Printf("using embedder: provider=%q", embedderProvider)

	// ----------------------------------------------------------
	// Resolve DSN: env var > config file
	// ----------------------------------------------------------
	dsn := os.Getenv("PG_DSN")
	if dsn == "" {
		dsn = cfg.Database.DSN
	}
	if dsn == "" {
		log.Fatalf("PG_DSN environment variable is required " +
			"(or set [database].dsn in bin/settings/server.toml)")
	}

	// ----------------------------------------------------------
	// Connect to PostgreSQL
	// ----------------------------------------------------------
	db, err := sqlx.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("failed to open PostgreSQL connection. %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping PostgreSQL. %v", err)
	}
	log.Printf("PostgreSQL connection established")

	// ----------------------------------------------------------
	// Create embedder client
	// ----------------------------------------------------------
	const vectorDimension = 1024
	var emb embedder.Embedder
	switch embedderProvider {
	case "zhipu":
		emb = embedder.NewZhipuEmbedder(embedderAPIKey, vectorDimension)
	default:
		emb = embedder.NewDashScopeEmbedder(embedderAPIKey, vectorDimension)
	}
	log.Printf("Embedder: %s (model=%s, dim=%d)", emb.Name(), emb.Model(), emb.Dimension())

	// ----------------------------------------------------------
	// Count total excerpts missing vectors
	// ----------------------------------------------------------
	var total int
	countSQL := `SELECT COUNT(*) FROM excerpts e
	             LEFT JOIN excerpt_vectors ev ON ev.excerpt_id = e.id
	             WHERE ev.excerpt_id IS NULL`
	if err := db.Get(&total, countSQL); err != nil {
		log.Fatalf("count missing excerpts failed. %v", err)
	}
	log.Printf("Found %d excerpts without embedding vectors", total)

	if total == 0 {
		log.Println("Nothing to do — all excerpts already have vectors")
		return
	}

	if f.dryRun {
		log.Printf("[dry-run] would process %d excerpts", total)
		if f.limit > 0 && f.limit < total {
			log.Printf("[dry-run] (limited to %d by --limit=%d)", f.limit, f.limit)
		}
		return
	}

	// Apply user limit if set.
	toProcess := total
	if f.limit > 0 && f.limit < toProcess {
		toProcess = f.limit
		log.Printf("Processing only %d excerpts (--limit=%d)", toProcess, f.limit)
	}

	// ----------------------------------------------------------
	// Process in batches
	// ----------------------------------------------------------
	batchSize := f.batchSize
	processed := 0
	errors := 0
	offset := 0

	ctx := context.Background()

	for {
		remaining := toProcess - processed
		if remaining <= 0 {
			break
		}
		if batchSize > remaining {
			batchSize = remaining
		}

		// Fetch a batch of excerpts that lack vectors.
		// Use keyset pagination via OFFSET — fine for a one-shot migration.
		query := `SELECT e.id, e.content, e.context_summary
		          FROM excerpts e
		          LEFT JOIN excerpt_vectors ev ON ev.excerpt_id = e.id
		          WHERE ev.excerpt_id IS NULL
		          ORDER BY e.id
		          LIMIT $1 OFFSET $2`
		var rows []excerptRow
		if err := db.Select(&rows, query, batchSize, offset); err != nil {
			log.Fatalf("query excerpts batch failed. %v", err)
		}
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			if f.limit > 0 && processed >= f.limit {
				break
			}

			// Build embedding text: content + context_summary (same pattern as excerpt_job.go:258).
			embeddingText := row.Content
			if row.ContextSummary != "" {
				embeddingText += " " + row.ContextSummary
			}

			vector, err := emb.Embed(ctx, embeddingText, embedderAPIKey)
			if err != nil {
				log.Printf("embed excerpt %d failed. %v", row.ID, err)
				errors++
				continue
			}

			// Store vector (idempotent: ON CONFLICT = UPDATE).
			insertSQL := `INSERT INTO excerpt_vectors(excerpt_id, embedding) VALUES($1, $2)
			              ON CONFLICT (excerpt_id) DO UPDATE SET embedding = $2`
			pgVec := pgvector.NewVector(vector)
			if _, err := db.Exec(insertSQL, row.ID, pgVec); err != nil {
				log.Printf("insert vector for excerpt %d failed. %v", row.ID, err)
				errors++
				continue
			}

			processed++
			if processed%100 == 0 {
				pct := float64(processed) / float64(toProcess) * 100
				log.Printf("progress: %d / %d (%.1f%%)  errors: %d",
					processed, toProcess, pct, errors)
			}

			// Small delay to avoid API rate limits.
			time.Sleep(50 * time.Millisecond)
		}

		offset += len(rows)
	}

	// ----------------------------------------------------------
	// Summary
	// ----------------------------------------------------------
	log.Printf("===== Backfill complete =====")
	log.Printf("  Total excerpts missing vectors: %d", total)
	log.Printf("  Processed (vectors inserted):   %d", processed)
	log.Printf("  Errors:                          %d", errors)

	if processed < total {
		log.Printf("  Remaining (not yet processed):  %d", total-processed)
	}
}

// Ensure pgx stdlib driver is registered with database/sql.
var _ = stdlib.GetDefaultDriver
