//go:build community

// Copyright 2022-Present Couchbase, Inc.
//
// Use of this software is governed by the Business Source License included
// in the file licenses/BSL-Couchbase.txt.  As of the Change Date specified
// in that file, in accordance with the Business Source License, use of this
// software will be governed by the Apache License, Version 2.0, included in
// the file licenses/APL2.txt.

package indexer

import (
	"github.com/couchbase/indexing/secondary/common"
	"time"
)

type MeteringThrottlingMgr struct {
}

func NewMeteringManager(nodeID string, config common.Config, supvCmdCh MsgChannel) (*MeteringThrottlingMgr, Message) {
	panic("MeteringManager::NewMeteringManager Not implemented for Community Edition")
	return nil, &MsgSuccess{}
}

func (m *MeteringThrottlingMgr) RegisterRestEndpoints() {
	panic("MeteringManager::RegisterRestEndpoints Not implemented for Community Edition")
}

func (m *MeteringThrottlingMgr) CheckWriteThrottle(bucket string) (
	result CheckResult, throttleTime time.Duration, err error) {
	panic("MeteringManager::Not implemented for Community Edition")
	return
}

func (m *MeteringThrottlingMgr) RecordWriteUnits(bucket string, bytes uint64, update bool) error {
	panic("MeteringManager::RecordWriteUnits Not implemented for Community Edition")
	return nil
}

func (m *MeteringThrottlingMgr) RefundWriteUnits(bucket string, bytes uint64) error {
	panic("MeteringManager::RefundWriteUnits Not implemented for Community Edition")
	return nil
}

func (m *MeteringThrottlingMgr) RecordReadUnits(bucket, user string, bytes uint64) error {
	panic("MeteringManager::RecordReadUnits Not implemented for Community Edition")
	return nil
}