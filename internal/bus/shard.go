package bus

import "github.com/cespare/xxhash/v2"

func ShardForAggregate(key string, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	return int(xxhash.Sum64String(key) % uint64(shardCount))
}
