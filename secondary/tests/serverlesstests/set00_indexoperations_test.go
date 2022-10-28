package serverlesstests

import (
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/couchbase/indexing/secondary/tests/framework/kvutility"
)

// TODO:Add special characters to bucket names
var buckets []string = []string{"bucket_1", "bucket_2"}
var collections []string = []string{"_default", "c1", "c2"}
var scope string = "_default"

// TODO: Add "#" character to primary index name
var indexes []string = []string{"idx_secondary", "idx_secondary_defer", "primary", "primary_defer", "idx_partitioned", "idx_partitioned_defer"}
var indexPartnIds [][]int = [][]int{[]int{0}, []int{0}, []int{0}, []int{0}, []int{1, 2, 3, 4, 5, 6, 7, 8}, []int{1, 2, 3, 4, 5, 6, 7, 8}}
var numDocs int = 100
var numScans int = 100

// When creating an index through N1QL, the index is expected
// to be created with a replica
func TestIndexPlacement(t *testing.T) {
	bucket := "bucket_1"
	scope := "_default"
	collection := "c1"
	index := "idx_1"

	// Create Bucket
	kvutility.CreateBucket(bucket, "sasl", "", clusterconfig.Username, clusterconfig.Password, clusterconfig.KVAddress, "256", "11213")
	kvutility.WaitForBucketCreation(bucket, clusterconfig.Username, clusterconfig.Password, []string{clusterconfig.Nodes[0], clusterconfig.Nodes[1], clusterconfig.Nodes[2]})

	manifest := kvutility.CreateCollection(bucket, scope, collection, clusterconfig.Username, clusterconfig.Password, clusterconfig.KVAddress)
	log.Printf("TestIndexPlacement: Manifest for bucket: %v, scope: %v, collection: %v is: %v", bucket, scope, collection, manifest)
	cid := kvutility.WaitForCollectionCreation(bucket, scope, collection, clusterconfig.Username, clusterconfig.Password, []string{clusterconfig.Nodes[0], clusterconfig.Nodes[1], clusterconfig.Nodes[2]}, manifest)

	CreateDocsForCollection(bucket, cid, numDocs)

	n1qlStatement := fmt.Sprintf("create index %v on `%v`.`%v`.`%v`(company)", index, bucket, scope, collection)
	execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, index, "Ready", t)

	waitForStatsUpdate()

	// Scan the index
	scanIndexReplicas(index, bucket, scope, collection, []int{0, 1}, numScans, numDocs, 1, t)
	kvutility.DeleteBucket(bucket, "", clusterconfig.Username, clusterconfig.Password, kvaddress)
	time.Sleep(bucketOpWaitDur * time.Second)
}

// This test creates a variety of indexes on 2 buckets, 3 collections per bucket
// and checks if all indexes are mapped to same shards
func TestShardIdMapping(t *testing.T) {

	for _, bucket := range buckets {
		kvutility.CreateBucket(bucket, "sasl", "", clusterconfig.Username, clusterconfig.Password, kvaddress, "100", "11213")
		kvutility.WaitForBucketCreation(bucket, clusterconfig.Username, clusterconfig.Password, []string{clusterconfig.Nodes[0], clusterconfig.Nodes[1], clusterconfig.Nodes[2]})

		for _, collection := range collections {
			var cid string
			if collection != "_default" { // default collection always exists
				manifest := kvutility.CreateCollection(bucket, scope, collection, clusterconfig.Username, clusterconfig.Password, clusterconfig.KVAddress)
				log.Printf("TestIndexPlacement: Manifest for bucket: %v, scope: %v, collection: %v is: %v", bucket, scope, collection, manifest)
				cid = kvutility.WaitForCollectionCreation(bucket, scope, collection, clusterconfig.Username, clusterconfig.Password, []string{clusterconfig.Nodes[0], clusterconfig.Nodes[1], clusterconfig.Nodes[2]}, manifest)
			} else {
				cid = "0" // CID of default collection
			}
			CreateDocsForCollection(bucket, cid, numDocs)

			// Create a variety of indexes on each bucket
			// Create a normal index
			n1qlStatement := fmt.Sprintf("create index %v on `%v`.`%v`.`%v`(age)", indexes[0], bucket, scope, collection)
			execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, indexes[0], "Ready", t)

			// Create an index with defer_build
			n1qlStatement = fmt.Sprintf("create index %v on `%v`.`%v`.`%v`(age) with {\"defer_build\":true}", indexes[1], bucket, scope, collection)
			execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, indexes[1], "Created", t)

			// Create a primary index
			n1qlStatement = fmt.Sprintf("create primary index `%v` on `%v`.`%v`.`%v`", indexes[2], bucket, scope, collection)
			execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, indexes[2], "Ready", t)

			// Create a primary index with defer_build:true
			n1qlStatement = fmt.Sprintf("create primary index `%v` on `%v`.`%v`.`%v` with {\"defer_build\":true}", indexes[3], bucket, scope, collection)
			execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, indexes[3], "Created", t)

			// Create a partitioned index
			n1qlStatement = fmt.Sprintf("create index %v on `%v`.`%v`.`%v`(age) partition by hash(meta().id)", indexes[4], bucket, scope, collection)
			execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, indexes[4], "Ready", t)

			// Create a partitioned index with defer_build:true
			n1qlStatement = fmt.Sprintf("create index %v on `%v`.`%v`.`%v`(age) partition by hash(meta().id)  with {\"defer_build\":true}", indexes[5], bucket, scope, collection)
			execN1qlAndWaitForStatus(n1qlStatement, bucket, scope, collection, indexes[5], "Created", t)
		}
	}

	validateShardIdMapping(clusterconfig.Nodes[1], t)
	validateShardIdMapping(clusterconfig.Nodes[2], t)
	waitForStatsUpdate()

	for _, bucket := range buckets {
		for _, collection := range collections {
			for i, index := range indexes {
				partns := indexPartnIds[i]
				if i%2 == 0 { // Scan all non-deferred indexes
					scanIndexReplicas(index, bucket, scope, collection, []int{0, 1}, numScans, numDocs, len(partns), t)
				}
			}
		}
	}
}
