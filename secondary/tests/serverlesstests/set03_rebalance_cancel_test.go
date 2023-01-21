package serverlesstests

import (
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/couchbase/indexing/secondary/testcode"
	tc "github.com/couchbase/indexing/secondary/tests/framework/common"
	"github.com/couchbase/indexing/secondary/tests/framework/kvutility"
	"github.com/couchbase/indexing/secondary/tests/framework/secondaryindex"
)

func TestRebalanceCancelTestsSetup(t *testing.T) {
	//a. Drop all secondary indexes
	e := secondaryindex.DropAllSecondaryIndexes(indexManagementAddress)
	tc.HandleError(e, "Error in DropAllSecondaryIndexes")
	for _, bucket := range buckets {
		kvutility.DeleteBucket(bucket, "", clusterconfig.Username, clusterconfig.Password, kvaddress)
	}
	time.Sleep(bucketOpWaitDur * time.Second)

	if absRebalStorageDirPath == "" {
		makeStorageDir(t)
	}

	//b. Remove all nodes from the cluster & keep only nodes[1], nodes[2]
	resetCluster(t)

	cleanupShardDir(t)

	// c. For each bucket, create indexes on c1 collection -> Just one collection
	// is sufficient for these tests as the goal is to validate rebalance failure
	// scenarios. Use partitioned index
	collection := "c1"
	for _, bucket := range buckets {
		kvutility.CreateBucket(bucket, "sasl", "", clusterconfig.Username, clusterconfig.Password, kvaddress, "100", "11213")
		kvutility.WaitForBucketCreation(bucket, clusterconfig.Username, clusterconfig.Password, []string{clusterconfig.Nodes[0], clusterconfig.Nodes[1], clusterconfig.Nodes[2]})

		manifest := kvutility.CreateCollection(bucket, scope, collection, clusterconfig.Username, clusterconfig.Password, clusterconfig.KVAddress)
		log.Printf("TestIndexPlacement: Manifest for bucket: %v, scope: %v, collection: %v is: %v", bucket, scope, collection, manifest)
		cid := kvutility.WaitForCollectionCreation(bucket, scope, collection, clusterconfig.Username, clusterconfig.Password, []string{clusterconfig.Nodes[0], clusterconfig.Nodes[1], clusterconfig.Nodes[2]}, manifest)

		CreateDocsForCollection(bucket, cid, numDocs)

		n1qlStatement := fmt.Sprintf("create index %v on `%v`.`%v`.`%v`(age)", indexes[0], bucket, scope, collection)
		execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, indexes[0], "Ready", t)

		// Create a partitioned index
		n1qlStatement = fmt.Sprintf("create index %v on `%v`.`%v`.`%v`(emalid) partition by hash(meta().id)", indexes[4], bucket, scope, collection)
		execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, indexes[4], "Ready", t)

		// Create a partitioned index with defer_build:true
		n1qlStatement = fmt.Sprintf("create index %v on `%v`.`%v`.`%v`(balance) partition by hash(meta().id)  with {\"defer_build\":true}", indexes[5], bucket, scope, collection)
		execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, indexes[5], "Created", t)
	}
	waitForStatsUpdate()
	// Scan indexes
	for _, bucket := range buckets {
		scanIndexReplicas(indexes[0], bucket, scope, collection, []int{0, 1}, numScans, numDocs, len(indexPartnIds[0]), t)
		scanIndexReplicas(indexes[4], bucket, scope, collection, []int{0, 1}, numScans, numDocs, len(indexPartnIds[4]), t)
	}

	// Enable testAction execution in the code
	err := secondaryindex.ChangeIndexerSettings("indexer.shardRebalance.execTestAction", true, clusterconfig.Username, clusterconfig.Password, kvaddress)
	tc.HandleError(err, "Error in ChangeIndexerSettings")
}

// Prior to this test, indexes existed on nodes[1] & nodes[2].
// This test will try to swap rebalance by adding nodes[3] & nodes[4],
// removing nodes[1], nodes[2]. Rebalance cancel is invoked in the code
// after transfer token move to state "ScheduleAck". Post rebalance
// failure, indexes should remain on nodes[1] & nodes[2]. The storage
// directory for rebalance should remain empty
func TestRebalanceCancelAtMasterShardTokenScheduleAck(t *testing.T) {
	log.Printf("In TestRebalanceCancelAtMasterShardTokenScheduleAck")

	tag := testcode.MASTER_SHARDTOKEN_SCHEDULEACK
	err := testcode.PostOptionsRequestToMetaKV("", clusterconfig.Username, clusterconfig.Password,
		tag, testcode.REBALANCE_CANCEL, "", 0)
	FailTestIfError(err, "Error while posting request to metaKV", t)

	defer func() {
		err = testcode.ResetMetaKV()
		FailTestIfError(err, "Error while resetting metakv", t)
	}()

	inNodes := []string{clusterconfig.Nodes[3], clusterconfig.Nodes[4]}
	outNodes := []string{clusterconfig.Nodes[1], clusterconfig.Nodes[2]}
	// Since rebalance is expected to fail, outNodes will be the final nodes in
	// the cluster. Hence populate "areInNodesFinal" to false
	testTwoNodeSwapRebalanceAndValidate(inNodes, outNodes, false, false, true, t)
}

// Prior to this test, all indexes existed on Nodes[1] & Nodes[2]
// This test will perform swap rebalance by removing Nodes[1] & Nodes[2]
// The Nodes[3] and Nodes[4] are added in earlier test - So, this test
// skips adding the nodes again.
// After finishing first transfer, indexer on Nodes[2] will cancel rebalance.
// This will lead to rebalance failure. After rebalance, all indexes
// should exist only on Nodes[1] and Nodes[2]
func TestRebalanceCancelAfterTransferOnSource(t *testing.T) {
	log.Printf("In TestRebalanceCancelAfterTransferOnSource")

	// Cancel rebalance from Nodes[2] after transfer is complete
	tag := testcode.SOURCE_SHARDTOKEN_AFTER_TRANSFER
	err := testcode.PostOptionsRequestToMetaKV(clusterconfig.Nodes[2], clusterconfig.Username, clusterconfig.Password,
		tag, testcode.REBALANCE_CANCEL, "", 0)
	FailTestIfError(err, "Error while posting request to metaKV", t)

	defer func() {
		err = testcode.ResetMetaKV()
		FailTestIfError(err, "Error while resetting metakv", t)
	}()

	inNodes := []string{clusterconfig.Nodes[3], clusterconfig.Nodes[4]}
	outNodes := []string{clusterconfig.Nodes[1], clusterconfig.Nodes[2]}

	// Since rebalance is expected to fail, outNodes will be the final nodes in
	// the cluster. Hence populate "areInNodesFinal" to false
	testTwoNodeSwapRebalanceAndValidate(inNodes, outNodes, false, true, true, t)
}

// This test will perform swap rebalance by removing Nodes[1] & Nodes[2]
// The Nodes[3] and Nodes[4] are added in earlier test - So, this test
// skips adding the nodes again.
// After finishing first restore, indexer on Nodes[3] will cancel rebalance.
// This will lead to rebalance failure. After rebalance, all indexes
// should exist only on Nodes[1] and Nodes[2]
func TestRebalanceCancelAfterRestoreOnDest(t *testing.T) {
	log.Printf("In TestRebalanceCancelAfterRestoreOnDest")

	// Cancel rebalance from indexer on Nodes[3] after transfer is complete
	tag := testcode.DEST_SHARDTOKEN_AFTER_RESTORE
	err := testcode.PostOptionsRequestToMetaKV(clusterconfig.Nodes[3], clusterconfig.Username, clusterconfig.Password,
		tag, testcode.REBALANCE_CANCEL, "", 0)
	FailTestIfError(err, "Error while posting request to metaKV", t)

	defer func() {
		err = testcode.ResetMetaKV()
		FailTestIfError(err, "Error while resetting metakv", t)
	}()

	inNodes := []string{clusterconfig.Nodes[3], clusterconfig.Nodes[4]}
	outNodes := []string{clusterconfig.Nodes[1], clusterconfig.Nodes[2]}

	// Since rebalance is expected to fail, outNodes will be the final nodes in
	// the cluster. Hence populate "areInNodesFinal" to false
	testTwoNodeSwapRebalanceAndValidate(inNodes, outNodes, false, true, true, t)
}

// This test will perform swap rebalance by removing Nodes[1] & Nodes[2]
// The Nodes[3] and Nodes[4] are added in earlier test - So, this test
// skips adding the nodes again.
// During restore, indexer on Nodes[3] will cancel rebalance during deferred index
// recovery. This will lead to rebalance failure. After rebalance, all indexes
// should exist only on Nodes[1] and Nodes[2]
func TestRebalanceCancelDuringDeferredIndexRecovery(t *testing.T) {
	log.Printf("In TestRebalanceCancelDuringDeferredIndexRecovery")

	// Cancel rebalance from indexer on Nodes[3] after transfer is complete
	tag := testcode.DEST_SHARDTOKEN_DURING_DEFERRED_INDEX_RECOVERY
	err := testcode.PostOptionsRequestToMetaKV(clusterconfig.Nodes[3], clusterconfig.Username, clusterconfig.Password,
		tag, testcode.REBALANCE_CANCEL, "", 0)
	FailTestIfError(err, "Error while posting request to metaKV", t)

	defer func() {
		err = testcode.ResetMetaKV()
		FailTestIfError(err, "Error while resetting metakv", t)
	}()

	inNodes := []string{clusterconfig.Nodes[3], clusterconfig.Nodes[4]}
	outNodes := []string{clusterconfig.Nodes[1], clusterconfig.Nodes[2]}

	// Since rebalance is expected to fail, outNodes will be the final nodes in
	// the cluster. Hence populate "areInNodesFinal" to false
	testTwoNodeSwapRebalanceAndValidate(inNodes, outNodes, false, true, true, t)
}

// This test will perform swap rebalance by removing Nodes[1] & Nodes[2]
// The Nodes[3] and Nodes[4] are added in earlier test - So, this test
// skips adding the nodes again.
// During restore, indexer on Nodes[3] will cancel rebalance during non-deferred index
// recovery. This will lead to rebalance failure. After rebalance, all indexes
// should exist only on Nodes[1] and Nodes[2]
func TestRebalanceCancelDuringNonDeferredIndexRecovery(t *testing.T) {
	log.Printf("In TestRebalanceCancelDuringNonDeferredIndexRecovery")

	// Cancel rebalance from indexer on Nodes[3] after transfer is complete
	tag := testcode.DEST_SHARDTOKEN_DURING_NON_DEFERRED_INDEX_RECOVERY
	err := testcode.PostOptionsRequestToMetaKV(clusterconfig.Nodes[3], clusterconfig.Username, clusterconfig.Password,
		tag, testcode.REBALANCE_CANCEL, "", 0)
	FailTestIfError(err, "Error while posting request to metaKV", t)

	defer func() {
		err = testcode.ResetMetaKV()
		FailTestIfError(err, "Error while resetting metakv", t)
	}()

	inNodes := []string{clusterconfig.Nodes[3], clusterconfig.Nodes[4]}
	outNodes := []string{clusterconfig.Nodes[1], clusterconfig.Nodes[2]}

	// Since rebalance is expected to fail, outNodes will be the final nodes in
	// the cluster. Hence populate "areInNodesFinal" to false
	testTwoNodeSwapRebalanceAndValidate(inNodes, outNodes, false, true, true, t)
}

// This test will perform swap rebalance by removing Nodes[1] & Nodes[2]
// The Nodes[3] and Nodes[4] are added in earlier test - So, this test
// skips adding the nodes again.
// During restore, indexer on Nodes[3] will cancel rebalance after index build is initiated
// recovery. This will lead to rebalance failure. After rebalance, all indexes
// should exist only on Nodes[1] and Nodes[2]
func TestRebalanceCancelDuringIndexBuild(t *testing.T) {
	log.Printf("In TestRebalanceCancelDuringIndexBuild")

	// Cancel rebalance from indexer on Nodes[3] during index build
	tag := testcode.DEST_SHARDTOKEN_DURING_INDEX_BUILD
	err := testcode.PostOptionsRequestToMetaKV(clusterconfig.Nodes[3], clusterconfig.Username, clusterconfig.Password,
		tag, testcode.REBALANCE_CANCEL, "", 0)
	FailTestIfError(err, "Error while posting request to metaKV", t)

	defer func() {
		err = testcode.ResetMetaKV()
		FailTestIfError(err, "Error while resetting metakv", t)
	}()

	inNodes := []string{clusterconfig.Nodes[3], clusterconfig.Nodes[4]}
	outNodes := []string{clusterconfig.Nodes[1], clusterconfig.Nodes[2]}

	// Since rebalance is expected to fail, outNodes will be the final nodes in
	// the cluster. Hence populate "areInNodesFinal" to false
	testTwoNodeSwapRebalanceAndValidate(inNodes, outNodes, false, true, true, t)
}
