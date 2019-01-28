package protobuf

import "errors"
import json "github.com/couchbase/indexing/secondary/common/json"

import c "github.com/couchbase/indexing/secondary/common"
import "github.com/golang/protobuf/proto"

// GetEntries implements queryport.client.ResponseReader{} method.
func (r *ResponseStream) GetEntries(dataEncFmt c.DataEncodingFormat) (*c.ScanResultEntries, [][]byte, error) {
	entries := r.GetIndexEntries()
	result := c.NewScanResultEntries(dataEncFmt)
	result.Make(len(entries))
	pkeys := make([][]byte, 0, len(entries))
	var dataEncFmtError error
	for _, entry := range entries {
		secKeyData := entry.GetEntryKey()
		if len(secKeyData) > 0 {
			if dataEncFmt == c.DATA_ENC_COLLATEJSON {
				result, dataEncFmtError = result.Append(secKeyData)
			} else if dataEncFmt == c.DATA_ENC_JSON {
				skey := make(c.SecondaryKey, 0)
				if err := json.Unmarshal(entry.GetEntryKey(), &skey); err != nil {
					return nil, nil, err
				}
				result, dataEncFmtError = result.Append(skey)
			} else {
				return nil, nil, c.ErrUnexpectedDataEncFmt
			}
		} else {
			result, dataEncFmtError = result.Append(nil)
		}

		if dataEncFmtError != nil {
			return nil, nil, dataEncFmtError
		}

		pkeys = append(pkeys, entry.GetPrimaryKey())
	}
	return result, pkeys, nil
}

// Error implements queryport.client.ResponseReader{} method.
func (r *ResponseStream) Error() error {
	if e := r.GetErr(); e != nil {
		if ee := e.GetError(); ee != "" {
			return errors.New(ee)
		}
	}
	return nil
}

// GetEntries implements queryport.client.ResponseReader{} method.
func (r *StreamEndResponse) GetEntries(dataEncFmt c.DataEncodingFormat) (*c.ScanResultEntries, [][]byte, error) {
	var results c.ScanResultEntries
	return &results, nil, nil
}

// Error implements queryport.client.ResponseReader{} method.
func (r *StreamEndResponse) Error() error {
	if e := r.GetErr(); e != nil {
		if ee := e.GetError(); ee != "" {
			return errors.New(ee)
		}
	}
	return nil
}

// Count implements common.IndexStatistics{} method.
func (s *IndexStatistics) Count() (int64, error) {
	return int64(s.GetKeysCount()), nil
}

// Min implements common.IndexStatistics{} method.
func (s *IndexStatistics) MinKey() (c.SecondaryKey, error) {
	skey := make(c.SecondaryKey, 0)
	if err := json.Unmarshal(s.GetKeyMin(), &skey); err != nil {
		return nil, err
	}
	return skey, nil
}

// Max implements common.IndexStatistics{} method.
func (s *IndexStatistics) MaxKey() (c.SecondaryKey, error) {
	skey := make(c.SecondaryKey, 0)
	if err := json.Unmarshal(s.GetKeyMax(), &skey); err != nil {
		return nil, err
	}
	return skey, nil
}

// DistinctCount implements common.IndexStatistics{} method.
func (s *IndexStatistics) DistinctCount() (int64, error) {
	return int64(s.GetUniqueKeysCount()), nil
}

// Bins implements common.IndexStatistics{} method.
func (s *IndexStatistics) Bins() ([]c.IndexStatistics, error) {
	return nil, nil
}

func NewTsConsistency(
	vbnos []uint16, seqnos []uint64, vbuuids []uint64,
	crc64 uint64) *TsConsistency {

	vbnos32 := make([]uint32, len(vbnos))
	for i, vbno := range vbnos {
		vbnos32[i] = uint32(vbno)
	}
	return &TsConsistency{
		Vbnos: vbnos32, Seqnos: seqnos, Vbuuids: vbuuids,
		Crc64: proto.Uint64(crc64),
	}
}
