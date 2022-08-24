//go:build !community
// +build !community

package indexer

// Copyright 2014-Present Couchbase, Inc.
//
// Use of this software is governed by the Business Source License included
// in the file licenses/BSL-Couchbase.txt.  As of the Change Date specified
// in that file, in accordance with the Business Source License, use of this
// software will be governed by the Apache License, Version 2.0, included in
// the file licenses/APL2.txt.

import (
	"fmt"

	"github.com/couchbase/indexing/secondary/common"
	"github.com/couchbase/plasma"
)

var errStorageCorrupted = fmt.Errorf("Storage corrupted and unrecoverable")

func NewPlasmaSlice(storage_dir string, log_dir string, path string, sliceId SliceId, idxDefn common.IndexDefn,
	idxInstId common.IndexInstId, partitionId common.PartitionId,
	isPrimary bool, numPartitions int,
	sysconf common.Config, idxStats *IndexStats, indexerStats *IndexerStats, isNew bool, isInitialBuild bool,
	meteringMgr *MeteringThrottlingMgr) (*plasmaSlice, error) {
	return newPlasmaSlice(storage_dir, log_dir, path, sliceId,
		idxDefn, idxInstId, partitionId, isPrimary, numPartitions,
		sysconf, idxStats, indexerStats, isNew, isInitialBuild, meteringMgr)
}

func DestroyPlasmaSlice(storageDir string, path string) error {
	return destroyPlasmaSlice(storageDir, path)
}

func ListPlasmaSlices() ([]string, error) {
	return listPlasmaSlices()
}

func BackupCorruptedPlasmaSlice(storageDir string, prefix string, rename func(string) (string, error), clean func(string)) error {
	return backupCorruptedPlasmaSlice(storageDir, prefix, rename, clean)
}

func RecoveryDone() {
	plasma.RecoveryDone()
}
