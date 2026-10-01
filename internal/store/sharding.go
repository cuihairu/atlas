package store

import (
	"hash/fnv"
	"strconv"
)

// CharacterShardStrategy decides which shard owns a character index key.
//
// The strategy must be pure and stateless: the same (accountID, numShards)
// pair must always map to the same shard, in every process, forever —
// otherwise characters "disappear" from their shard after a restart.
type CharacterShardStrategy interface {
	// ShardForAccount maps an account to a shard index in [0, numShards).
	// numShards is always >= 1.
	ShardForAccount(accountID int64, numShards int) int
}

// HashShardStrategy distributes accounts by the FNV-1a hash of the account
// ID modulo the shard count. It needs no external state and gives a stable,
// near-uniform distribution; changing numShards reassigns most keys, so
// rescaling requires the resharding tool (store/sharded.Reshard).
type HashShardStrategy struct{}

// ShardForAccount implements CharacterShardStrategy.
func (HashShardStrategy) ShardForAccount(accountID int64, numShards int) int {
	if numShards <= 1 {
		return 0
	}
	h := fnv.New64a()
	h.Write([]byte(strconv.FormatInt(accountID, 10)))
	return int(h.Sum64() % uint64(numShards))
}
