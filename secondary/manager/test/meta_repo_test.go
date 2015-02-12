// Copyright (c) 2014 Couchbase, Inc.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
// except in compliance with the License. You may obtain a copy of the License at
//   http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the
// License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
// either express or implied. See the License for the specific language governing permissions
// and limitations under the License.

package test

import (
	gometaL "github.com/couchbase/gometa/log"
	"github.com/couchbase/indexing/secondary/common"
	"github.com/couchbase/indexing/secondary/manager"
	fdb "github.com/couchbaselabs/goforestdb"
	"os"
	"testing"
	"time"
)

// For this test, use index definition from 100 - 110

func TestMetadataRepoForIndexDefn(t *testing.T) {

	common.LogEnable()
	logging.SetLogLevel(logging.LogLevelTrace)

	gometaL.LogEnable()
	gometaL.SetLogLevel(gometaL.LogLevelTrace)
	gometaL.SetPrefix("Indexing/Gometa")

	logging.Infof("Start TestMetadataRepo *********************************************************")

	/*
		var addr = "localhost:9885"
		var leader = "localhost:9884"

		repo, err := manager.NewMetadataRepo(addr, leader, "./config.json", nil)
		if err != nil {
			t.Fatal(err)
		}
		runTest(repo, t)
	*/

	os.MkdirAll("./data/", os.ModePerm)
	repo, _, err := manager.NewLocalMetadataRepo("localhost:5002", nil, nil, "./data/MetadataStore")
	if err != nil {
		t.Fatal(err)
	}
	runTest(repo, t)
}

func runTest(repo *manager.MetadataRepo, t *testing.T) {

	// clean up
	repo.DropIndexById(common.IndexDefnId(100))
	repo.DropIndexById(common.IndexDefnId(101))
	repo.DropIndexById(common.IndexDefnId(102))
	repo.DropIndexById(common.IndexDefnId(103))

	time.Sleep(time.Duration(1000) * time.Millisecond)

	// Add a new index definition : 100
	idxDefn := &common.IndexDefn{
		DefnId:          common.IndexDefnId(100),
		Name:            "metadata_repo_test",
		Using:           common.ForestDB,
		Bucket:          "Default",
		IsPrimary:       false,
		SecExprs:        []string{"Testing"},
		ExprType:        common.N1QL,
		PartitionScheme: common.HASH,
		PartitionKey:    "Testing"}

	if err := repo.CreateIndex(idxDefn); err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Duration(1000) * time.Millisecond)

	// Get the index definition	by name
	idxDefn, err := repo.GetIndexDefnById(common.IndexDefnId(100))
	if err != nil {
		t.Fatal(err)
	}

	if idxDefn == nil {
		t.Fatal("Cannot find index definition")
	}

	// Delete the index definition by name
	if err := repo.DropIndexById(common.IndexDefnId(100)); err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Duration(1000) * time.Millisecond)

	// Get the index definition	by name
	idxDefn, err = repo.GetIndexDefnById(common.IndexDefnId(100))

	if idxDefn != nil {
		t.Fatal("Find deleted index definition")
	}

	// Add a new index definition : 101
	idxDefn = &common.IndexDefn{
		DefnId:          common.IndexDefnId(101),
		Name:            "metadata_repo_test_2",
		Using:           common.ForestDB,
		Bucket:          "Default",
		IsPrimary:       false,
		SecExprs:        []string{"Testing"},
		ExprType:        common.N1QL,
		PartitionScheme: common.HASH,
		PartitionKey:    "Testing"}

	if err := repo.CreateIndex(idxDefn); err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Duration(1000) * time.Millisecond)

	// Get the index definition by Id
	idxDefn, err = repo.GetIndexDefnById(common.IndexDefnId(101))
	if err != nil {
		t.Fatal(err)
	}

	if idxDefn == nil {
		t.Fatal("Cannot find index definition")
	}

	// Delete the index definition by Id
	if err := repo.DropIndexById(common.IndexDefnId(101)); err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Duration(1000) * time.Millisecond)

	// Get the index definition by Id
	idxDefn, err = repo.GetIndexDefnById(common.IndexDefnId(101))

	if idxDefn != nil {
		t.Fatal("Find deleted index definition")
	}

	// Add a new index definition : 102
	idxDefn = &common.IndexDefn{
		DefnId:          common.IndexDefnId(102),
		Name:            "metadata_repo_test_3",
		Using:           common.ForestDB,
		Bucket:          "Default",
		IsPrimary:       false,
		SecExprs:        []string{"Testing"},
		ExprType:        common.N1QL,
		PartitionScheme: common.HASH,
		PartitionKey:    "Testing"}

	if err = repo.CreateIndex(idxDefn); err != nil {
		t.Fatal(err)
	}

	// Add a new index definition : 103
	idxDefn = &common.IndexDefn{
		DefnId:          common.IndexDefnId(103),
		Name:            "metadata_repo_test_4",
		Using:           common.ForestDB,
		Bucket:          "Default",
		IsPrimary:       false,
		SecExprs:        []string{"Testing"},
		ExprType:        common.N1QL,
		PartitionScheme: common.HASH,
		PartitionKey:    "Testing"}

	if err = repo.CreateIndex(idxDefn); err != nil {
		t.Fatal(err)
	}

	// Test the iterator
	iter, err := repo.NewIterator()
	if err != nil {
		t.Fatal("Fail to get the iterator")
	}

	found := false
	for {
		key, defn, err := iter.Next()
		if err != nil {
			if err != fdb.RESULT_ITERATOR_FAIL {
				logging.Infof("error during iteration %s", err.Error())
			}
			break
		}

		logging.Infof("key during iteration %s", key)
		if key == "103" && defn.DefnId == common.IndexDefnId(103) {
			found = true
		}
	}

	if !found {
		t.Fatal("Cannot find index defn 'metadata_repo_test_3' in iterator")
	}

	time.Sleep(time.Duration(1000) * time.Millisecond)

	// test loal value

	if err := repo.SetLocalValue("testLocalValue1", "testLocalValue1"); err != nil {
		t.Fatal("Fail to set local value" + err.Error())
	}

	value, err := repo.GetLocalValue("testLocalValue1")
	if err != nil {
		t.Fatal("Fail to set local value" + err.Error())
	}
	if value != "testLocalValue1" {
		t.Fatal("Fail to set local value : Return value is different")
	}

	logging.Infof("Stop TestMetadataRepo. Tearing down *********************************************************")

	// clean up
	repo.DropIndexById(common.IndexDefnId(100))
	repo.DropIndexById(common.IndexDefnId(101))
	repo.DropIndexById(common.IndexDefnId(102))
	repo.DropIndexById(common.IndexDefnId(103))

	time.Sleep(time.Duration(1000) * time.Millisecond)
}
