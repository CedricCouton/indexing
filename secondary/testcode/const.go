package testcode

type TestActionTag int

const (
	MASTER_SHARDTOKEN_SCHEDULEACK TestActionTag = iota + 1
	SOURCE_SHARDTOKEN_AFTER_TRANSFER
)

type TestAction int

const (
	NONE                TestAction = iota // No action to be taken
	INDEXER_PANIC                         // Panic indexer at the tag
	REBALANCE_CANCEL                      // Cancel rebalance at the tag
	EXEC_N1QL_STATEMENT                   // Execute N1QL statement at the tag
	SLEEP                                 // Sleep at the tag
)

func isMasterTag(tag TestActionTag) bool {
	switch tag {
	case MASTER_SHARDTOKEN_SCHEDULEACK:
		return true
	}
	return false
}
