// Copyright (c) 2014 Couchbase, Inc.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
// except in compliance with the License. You may obtain a copy of the License at
//   http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the
// License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
// either express or implied. See the License for the specific language governing permissions
// and limitations under the License.

package indexer

import (
	"github.com/couchbaselabs/goforestdb"
)

//ForestDBIterator taken from
//https://github.com/couchbaselabs/bleve/blob/master/index/store/goforestdb/iterator.go
type ForestDBIterator struct {
	db    *forestdb.KVStore
	valid bool
	curr  *forestdb.Doc
	iter  *forestdb.Iterator
}

func newForestDBIterator(db *forestdb.KVStore) *ForestDBIterator {
	rv := ForestDBIterator{
		db: db,
	}
	return &rv
}

func (f *ForestDBIterator) SeekFirst() {
	if f.iter != nil {
		f.iter.Close()
		f.iter = nil
	}
	var err error
	f.iter, err = f.db.IteratorInit([]byte{}, nil, forestdb.ITR_NONE|forestdb.ITR_NO_DELETES)
	if err != nil {
		f.valid = false
		return
	}
	f.valid = true
	f.Next()
}

func (f *ForestDBIterator) Seek(key []byte) {
	if f.iter != nil {
		f.iter.Close()
		f.iter = nil
	}
	var err error
	f.iter, err = f.db.IteratorInit(key, nil, forestdb.ITR_NONE|forestdb.ITR_NO_DELETES)
	if err != nil {
		f.valid = false
		return
	}
	f.valid = true
	f.Next()
}

func (f *ForestDBIterator) Next() {
	var err error
	f.curr, err = f.iter.Next()
	if err != nil {
		f.valid = false
	}
}

func (f *ForestDBIterator) Current() ([]byte, []byte, bool) {
	if f.valid {
		return f.Key(), f.Value(), true
	}
	return nil, nil, false
}

func (f *ForestDBIterator) Key() []byte {
	if f.valid && f.curr != nil {
		return f.curr.Key()
	}
	return nil
}

func (f *ForestDBIterator) Value() []byte {
	if f.valid && f.curr != nil {
		return f.curr.Body()
	}
	return nil
}

func (f *ForestDBIterator) Valid() bool {
	return f.valid
}

func (f *ForestDBIterator) Close() {
	f.valid = false
	f.iter.Close()
	f.iter = nil
}
