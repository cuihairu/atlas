// atlas-reshard copies the character index between store layouts (TODO
// v0.1.16): single PostgreSQL database → N sharded databases, N shards → M
// shards, or any collapse back to a single store. The copy is idempotent
// (destination upserts), so an interrupted run can simply be repeated.
//
// Usage:
//
//	atlas-reshard --from-dsn postgres://... [--to-dsn postgres://...,...] \
//	    [--shards N] [--batch 500]
//
// --from-dsn      source PostgreSQL connection string (single store).
// --to-dsn        comma-separated destination connection strings. One entry
//
//	means a plain single-store copy; N entries build an
//	N-way sharded destination (account-hash routed).
//
// --shards        only valid together with a single --to-dsn: replicate the
//
//	logical shard layout over one physical store (N logical
//	shards, same physical tables) — useful to verify routing
//	before splitting storage.
//
// --batch         source read page size (default 500).
//
// The tool never deletes source data and never switches live traffic: stop
// or quiesce directory writes, run the copy, verify the reported count, then
// repoint ATLAS_CHAR_SHARDS / the directory store.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/postgres"
	"github.com/cuihairu/atlas/internal/store/sharded"
)

func main() {
	var (
		fromDSN = flag.String("from-dsn", "", "source PostgreSQL connection string")
		toDSN   = flag.String("to-dsn", "", "comma-separated destination connection strings")
		shards  = flag.Int("shards", 0, "logical shard count over a single destination store")
		batch   = flag.Int("batch", 500, "source read page size")
	)
	flag.Parse()

	if *fromDSN == "" || *toDSN == "" {
		fmt.Fprintln(os.Stderr, "atlas-reshard: --from-dsn and --to-dsn are required")
		flag.Usage()
		os.Exit(2)
	}
	if *shards < 0 {
		fmt.Fprintln(os.Stderr, "atlas-reshard: --shards must be >= 0")
		os.Exit(2)
	}
	if *shards > 0 && len(strings.Split(*toDSN, ",")) > 1 {
		fmt.Fprintln(os.Stderr, "atlas-reshard: --shards applies only to a single --to-dsn")
		os.Exit(2)
	}

	ctx := context.Background()

	srcPool, err := pgxpool.New(ctx, *fromDSN)
	if err != nil {
		fatal("open source: %v", err)
	}
	defer srcPool.Close()
	src := postgres.New(srcPool)

	dst, closeDst, err := openDestination(ctx, *toDSN, *shards)
	if err != nil {
		fatal("open destination: %v", err)
	}
	defer closeDst()

	fmt.Printf("copying character index (batch %d)...\n", *batch)
	n, err := sharded.Reshard(ctx, src, dst, *batch)
	if err != nil {
		fatal("reshard after %d characters: %v", n, err)
	}
	fmt.Printf("done: %d characters copied.\n", n)
}

// openDestination builds the destination store from the DSN list: one entry
// (plus optional --shards) or one pool per entry for physical sharding.
func openDestination(ctx context.Context, toDSN string, logicalShards int) (store.CharacterStore, func(), error) {
	dsns := strings.Split(toDSN, ",")

	var dst store.CharacterStore
	var pools []*pgxpool.Pool
	closeAll := func() {
		for _, p := range pools {
			p.Close()
		}
	}

	if len(dsns) == 1 {
		pool, err := pgxpool.New(ctx, dsns[0])
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		pools = append(pools, pool)
		single := postgres.New(pool)
		if logicalShards > 1 {
			shardStores := make([]store.CharacterStore, logicalShards)
			for i := range shardStores {
				shardStores[i] = single
			}
			dst = sharded.New(shardStores, store.HashShardStrategy{})
		} else {
			dst = single
		}
		return dst, closeAll, nil
	}

	shardStores := make([]store.CharacterStore, 0, len(dsns))
	for _, dsn := range dsns {
		pool, err := pgxpool.New(ctx, strings.TrimSpace(dsn))
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		pools = append(pools, pool)
		shardStores = append(shardStores, postgres.New(pool))
	}
	return sharded.New(shardStores, store.HashShardStrategy{}), closeAll, nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "atlas-reshard: "+format+"\n", args...)
	os.Exit(1)
}
