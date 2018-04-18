// Package queryport provides a simple library to spawn a queryport and access
// queryport via passive client API.
//
// ---> Request                 ---> Request
//      <--- Response                <--- Response
//      <--- Response                <--- Response
//      ...                     ---> EndStreamRequest
//      <--- StreamEndResponse       <--- Response (residue)
//                                   <--- StreamEndResponse

package client

import "errors"
import "fmt"
import "io"
import "net"
import "time"
import json "github.com/couchbase/indexing/secondary/common/json"
import "sync/atomic"

import "github.com/couchbase/indexing/secondary/logging"
import "github.com/couchbase/indexing/secondary/common"
import protobuf "github.com/couchbase/indexing/secondary/protobuf/query"
import "github.com/couchbase/indexing/secondary/transport"
import "github.com/golang/protobuf/proto"

// GsiScanClient for scan operations.
type GsiScanClient struct {
	queryport string
	pool      *connectionPool
	// config params
	maxPayload         int // TODO: what if it exceeds ?
	readDeadline       time.Duration
	writeDeadline      time.Duration
	poolSize           int
	poolOverflow       int
	cpTimeout          time.Duration
	cpAvailWaitTimeout time.Duration
	logPrefix          string

	serverVersion uint32
}

func NewGsiScanClient(queryport string, config common.Config) (*GsiScanClient, error) {
	t := time.Duration(config["connPoolAvailWaitTimeout"].Int())
	c := &GsiScanClient{
		queryport:          queryport,
		maxPayload:         config["maxPayload"].Int(),
		readDeadline:       time.Duration(config["readDeadline"].Int()),
		writeDeadline:      time.Duration(config["writeDeadline"].Int()),
		poolSize:           config["settings.poolSize"].Int(),
		poolOverflow:       config["settings.poolOverflow"].Int(),
		cpTimeout:          time.Duration(config["connPoolTimeout"].Int()),
		cpAvailWaitTimeout: t,
		logPrefix:          fmt.Sprintf("[GsiScanClient:%q]", queryport),
	}
	c.pool = newConnectionPool(
		queryport, c.poolSize, c.poolOverflow, c.maxPayload, c.cpTimeout,
		c.cpAvailWaitTimeout)
	logging.Infof("%v started ...\n", c.logPrefix)

	if version, err := c.Helo(); err == nil || err == io.EOF {
		atomic.StoreUint32(&c.serverVersion, version)
	} else {
		c.pool.Close()
		return nil, fmt.Errorf("%s: unable to obtain server version", queryport)
	}

	return c, nil
}

func (c *GsiScanClient) RefreshServerVersion() {
	// refresh the version ONLY IF there is no error, so we absolutely
	// know we have right version.
	if version, err := c.Helo(); err == nil {
		if version != atomic.LoadUint32(&c.serverVersion) {
			atomic.StoreUint32(&c.serverVersion, version)
		}
	}
}

func (c *GsiScanClient) NeedSessionConsVector() bool {
	return atomic.LoadUint32(&c.serverVersion) == 0
}

func (c *GsiScanClient) Helo() (uint32, error) {
	req := &protobuf.HeloRequest{
		Version: proto.Uint32(uint32(protobuf.ProtobufVersion())),
	}

	resp, err := c.doRequestResponse(req, "")
	if err != nil {
		return 0, err
	}
	heloResp := resp.(*protobuf.HeloResponse)
	return heloResp.GetVersion(), nil
}

// LookupStatistics for a single secondary-key.
func (c *GsiScanClient) LookupStatistics(
	defnID uint64, value common.SecondaryKey) (common.IndexStatistics, error) {

	// serialize lookup value.
	val, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	req := &protobuf.StatisticsRequest{
		DefnID: proto.Uint64(defnID),
		Span:   &protobuf.Span{Equals: [][]byte{val}},
	}
	resp, err := c.doRequestResponse(req, "")
	if err != nil {
		return nil, err
	}
	statResp := resp.(*protobuf.StatisticsResponse)
	if statResp.GetErr() != nil {
		err = errors.New(statResp.GetErr().GetError())
		return nil, err
	}
	return statResp.GetStats(), nil
}

// RangeStatistics for index range.
func (c *GsiScanClient) RangeStatistics(
	defnID uint64, low, high common.SecondaryKey,
	inclusion Inclusion) (common.IndexStatistics, error) {

	// serialize low and high values.
	l, err := json.Marshal(low)
	if err != nil {
		return nil, err
	}
	h, err := json.Marshal(high)
	if err != nil {
		return nil, err
	}

	req := &protobuf.StatisticsRequest{
		DefnID: proto.Uint64(defnID),
		Span: &protobuf.Span{
			Range: &protobuf.Range{
				Low: l, High: h, Inclusion: proto.Uint32(uint32(inclusion)),
			},
		},
	}
	resp, err := c.doRequestResponse(req, "")
	if err != nil {
		return nil, err
	}
	statResp := resp.(*protobuf.StatisticsResponse)
	if statResp.GetErr() != nil {
		err = errors.New(statResp.GetErr().GetError())
		return nil, err
	}
	return statResp.GetStats(), nil
}

// Lookup scan index between low and high.
func (c *GsiScanClient) Lookup(
	defnID uint64, requestId string, values []common.SecondaryKey,
	distinct bool, limit int64,
	cons common.Consistency, vector *TsConsistency,
	callb ResponseHandler,
	rollbackTime int64,
	partitions []common.PartitionId) (error, bool) {

	// serialize lookup value.
	equals := make([][]byte, 0, len(values))
	for _, value := range values {
		val, err := json.Marshal(value)
		if err != nil {
			return err, false
		}
		equals = append(equals, val)
	}

	connectn, err := c.pool.Get()
	if err != nil {
		return err, false
	}
	healthy := true
	closeStream := false
	conn, pkt := connectn.conn, connectn.pkt
	defer func() {
		go func() {
			if closeStream {
				_, healthy = c.closeStream(conn, pkt, requestId)
			}
			c.pool.Return(connectn, healthy)
		}()
	}()

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.ScanRequest{
		DefnID:       proto.Uint64(defnID),
		RequestId:    proto.String(requestId),
		Span:         &protobuf.Span{Equals: equals},
		Distinct:     proto.Bool(distinct),
		Limit:        proto.Int64(limit),
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
		Sorted:       proto.Bool(true),
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}

	// ---> protobuf.ScanRequest
	if err := c.sendRequest(conn, pkt, req); err != nil {
		fmsg := "%v Lookup(%v) request transport failed `%v`\n"
		logging.Errorf(fmsg, c.logPrefix, requestId, err)
		healthy = false
		return err, false
	}

	cont, partial := true, false
	for cont {
		// <--- protobuf.ResponseStream
		cont, healthy, err, closeStream = c.streamResponse(conn, pkt, callb, requestId)
		if err != nil { // if err, cont should have been set to false
			fmsg := "%v Lookup(%s) response failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, err)
		} else { // partially succeeded
			partial = true
		}
	}
	return err, partial
}

// Range scan index between low and high.
func (c *GsiScanClient) Range(
	defnID uint64, requestId string, low, high common.SecondaryKey, inclusion Inclusion,
	distinct bool, limit int64, cons common.Consistency, vector *TsConsistency,
	callb ResponseHandler, rollbackTime int64, partitions []common.PartitionId) (error, bool) {

	// serialize low and high values.
	l, err := json.Marshal(low)
	if err != nil {
		return err, false
	}
	h, err := json.Marshal(high)
	if err != nil {
		return err, false
	}

	connectn, err := c.pool.Get()
	if err != nil {
		return err, false
	}
	healthy := true
	closeStream := false
	conn, pkt := connectn.conn, connectn.pkt
	defer func() {
		go func() {
			if closeStream {
				_, healthy = c.closeStream(conn, pkt, requestId)
			}
			c.pool.Return(connectn, healthy)
		}()
	}()

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.ScanRequest{
		DefnID:    proto.Uint64(defnID),
		RequestId: proto.String(requestId),
		Span: &protobuf.Span{
			Range: &protobuf.Range{
				Low: l, High: h, Inclusion: proto.Uint32(uint32(inclusion)),
			},
		},
		Distinct:     proto.Bool(distinct),
		Limit:        proto.Int64(limit),
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
		Sorted:       proto.Bool(true),
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}
	// ---> protobuf.ScanRequest
	if err := c.sendRequest(conn, pkt, req); err != nil {
		fmsg := "%v Range(%v) request transport failed `%v`\n"
		logging.Errorf(fmsg, c.logPrefix, requestId, err)
		healthy = false
		return err, false
	}

	cont, partial := true, false
	for cont {
		// <--- protobuf.ResponseStream
		cont, healthy, err, closeStream = c.streamResponse(conn, pkt, callb, requestId)
		if err != nil { // if err, cont should have been set to false
			fmsg := "%v Range(%v) response failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, err)
		} else { // partial succeeded
			partial = true
		}
	}
	return err, partial
}

// Range scan index between low and high.
func (c *GsiScanClient) RangePrimary(
	defnID uint64, requestId string, low, high []byte, inclusion Inclusion,
	distinct bool, limit int64, cons common.Consistency, vector *TsConsistency,
	callb ResponseHandler, rollbackTime int64, partitions []common.PartitionId) (error, bool) {

	connectn, err := c.pool.Get()
	if err != nil {
		return err, false
	}
	healthy := true
	closeStream := false
	conn, pkt := connectn.conn, connectn.pkt
	defer func() {
		go func() {
			if closeStream {
				_, healthy = c.closeStream(conn, pkt, requestId)
			}
			c.pool.Return(connectn, healthy)
		}()
	}()

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.ScanRequest{
		DefnID:    proto.Uint64(defnID),
		RequestId: proto.String(requestId),
		Span: &protobuf.Span{
			Range: &protobuf.Range{
				Low: low, High: high,
				Inclusion: proto.Uint32(uint32(inclusion)),
			},
		},
		Distinct:     proto.Bool(distinct),
		Limit:        proto.Int64(limit),
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
		Sorted:       proto.Bool(true),
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}
	// ---> protobuf.ScanRequest
	if err := c.sendRequest(conn, pkt, req); err != nil {
		fmsg := "%v RangePrimary(%v) request transport failed `%v`\n"
		logging.Errorf(fmsg, c.logPrefix, requestId, err)
		healthy = false
		return err, false
	}

	cont, partial := true, false
	for cont {
		// <--- protobuf.ResponseStream
		cont, healthy, err, closeStream = c.streamResponse(conn, pkt, callb, requestId)
		if err != nil { // if err, cont should have been set to false
			fmsg := "%v RangePrimary(%v) response failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, err)
		} else {
			partial = true
		}
	}
	return err, partial
}

// ScanAll for full table scan.
func (c *GsiScanClient) ScanAll(
	defnID uint64, requestId string, limit int64,
	cons common.Consistency, vector *TsConsistency,
	callb ResponseHandler, rollbackTime int64, partitions []common.PartitionId) (error, bool) {

	connectn, err := c.pool.Get()
	if err != nil {
		return err, false
	}
	healthy := true
	closeStream := false
	conn, pkt := connectn.conn, connectn.pkt
	defer func() {
		go func() {
			if closeStream {
				_, healthy = c.closeStream(conn, pkt, requestId)
			}
			c.pool.Return(connectn, healthy)
		}()
	}()

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.ScanAllRequest{
		DefnID:       proto.Uint64(defnID),
		RequestId:    proto.String(requestId),
		Limit:        proto.Int64(limit),
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}
	if err := c.sendRequest(conn, pkt, req); err != nil {
		fmsg := "%v ScanAll(%v) request transport failed `%v`\n"
		logging.Errorf(fmsg, c.logPrefix, requestId, err)
		healthy = false
		return err, false
	}

	cont, partial := true, false
	for cont {
		// <--- protobuf.ResponseStream
		cont, healthy, err, closeStream = c.streamResponse(conn, pkt, callb, requestId)
		if err != nil { // if err, cont should have been set to false
			fmsg := "%v ScanAll(%v) response failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, err)
		} else {
			partial = true
		}
	}
	return err, partial
}

func (c *GsiScanClient) MultiScan(
	defnID uint64, requestId string, scans Scans,
	reverse, distinct bool, projection *IndexProjection, offset, limit int64,
	cons common.Consistency, vector *TsConsistency,
	callb ResponseHandler, rollbackTime int64, partitions []common.PartitionId) (error, bool) {

	// serialize scans
	protoScans := make([]*protobuf.Scan, len(scans))
	for i, scan := range scans {
		if scan != nil {
			var equals [][]byte
			var filters []*protobuf.CompositeElementFilter

			// If Seek is there, then do not marshall Range
			if len(scan.Seek) > 0 {
				equals = make([][]byte, len(scan.Seek))
				for i, seek := range scan.Seek {
					s, err := json.Marshal(seek)
					if err != nil {
						return err, false
					}
					equals[i] = s
				}
			} else {
				filters = make([]*protobuf.CompositeElementFilter, len(scan.Filter))
				if scan.Filter != nil {
					for j, f := range scan.Filter {
						var l, h []byte
						var err error
						if f.Low != common.MinUnbounded { // Do not encode if unbounded
							l, err = json.Marshal(f.Low)
							if err != nil {
								return err, false
							}
						}

						if f.High != common.MaxUnbounded { // Do not encode if unbounded
							h, err = json.Marshal(f.High)
							if err != nil {
								return err, false
							}
						}

						fl := &protobuf.CompositeElementFilter{
							Low: l, High: h, Inclusion: proto.Uint32(uint32(f.Inclusion)),
						}

						filters[j] = fl
					}
				}
			}
			s := &protobuf.Scan{
				Filters: filters,
				Equals:  equals,
			}
			protoScans[i] = s
		}
	}

	//IndexProjection
	var protoProjection *protobuf.IndexProjection
	if projection != nil {
		protoProjection = &protobuf.IndexProjection{
			EntryKeys:  projection.EntryKeys,
			PrimaryKey: proto.Bool(projection.PrimaryKey),
		}
	}

	connectn, err := c.pool.Get()
	if err != nil {
		return err, false
	}
	healthy := true
	closeStream := false
	conn, pkt := connectn.conn, connectn.pkt
	defer func() {
		go func() {
			if closeStream {
				_, healthy = c.closeStream(conn, pkt, requestId)
			}
			c.pool.Return(connectn, healthy)
		}()
	}()

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.ScanRequest{
		DefnID: proto.Uint64(defnID),
		Span: &protobuf.Span{
			Range: nil,
		},
		RequestId:       proto.String(requestId),
		Distinct:        proto.Bool(distinct),
		Limit:           proto.Int64(limit),
		Cons:            proto.Uint32(uint32(cons)),
		Scans:           protoScans,
		Indexprojection: protoProjection,
		Reverse:         proto.Bool(reverse),
		Offset:          proto.Int64(offset),
		RollbackTime:    proto.Int64(rollbackTime),
		PartitionIds:    partnIds,
		Sorted:          proto.Bool(true),
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}
	// ---> protobuf.ScanRequest
	if err := c.sendRequest(conn, pkt, req); err != nil {
		fmsg := "%v Range(%v) request transport failed `%v`\n"
		logging.Errorf(fmsg, c.logPrefix, requestId, err)
		healthy = false
		return err, false
	}

	cont, partial := true, false
	for cont {
		// <--- protobuf.ResponseStream
		cont, healthy, err, closeStream = c.streamResponse(conn, pkt, callb, requestId)
		if err != nil { // if err, cont should have been set to false
			fmsg := "%v Scans(%v) response failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, err)
		} else { // partial succeeded
			partial = true
		}
	}
	return err, partial
}

func (c *GsiScanClient) MultiScanPrimary(
	defnID uint64, requestId string, scans Scans,
	reverse, distinct bool, projection *IndexProjection, offset, limit int64,
	cons common.Consistency, vector *TsConsistency,
	callb ResponseHandler, rollbackTime int64, partitions []common.PartitionId) (error, bool) {

	var what string
	// serialize scans
	protoScans := make([]*protobuf.Scan, 0)
	for _, scan := range scans {
		if scan != nil {
			var equals [][]byte
			var filters []*protobuf.CompositeElementFilter

			// If Seek is there, then ignore Range
			if len(scan.Seek) > 0 {
				var k []byte
				key := scan.Seek[0]
				if k, what = curePrimaryKey(key); what == "after" {
					continue
				}
				equals = [][]byte{k}
			} else {
				filters = make([]*protobuf.CompositeElementFilter, 0)
				skip := false
				if scan.Filter != nil {
					for _, f := range scan.Filter {
						var l, h []byte
						if f.Low != common.MinUnbounded { // Ignore if unbounded
							if l, what = curePrimaryKey(f.Low); what == "after" {
								skip = true
								break
							}
						}
						if f.High != common.MaxUnbounded { // Ignore if unbounded
							if h, what = curePrimaryKey(f.High); what == "before" {
								skip = true
								break
							}
						}

						fl := &protobuf.CompositeElementFilter{
							Low: l, High: h, Inclusion: proto.Uint32(uint32(f.Inclusion)),
						}

						filters = append(filters, fl)
					}
					if skip {
						continue
					}
				}
			}
			s := &protobuf.Scan{
				Filters: filters,
				Equals:  equals,
			}
			protoScans = append(protoScans, s)
		}
	}

	if len(protoScans) == 0 {
		return nil, true
	}

	//IndexProjection
	var protoProjection *protobuf.IndexProjection
	if projection != nil {
		protoProjection = &protobuf.IndexProjection{
			EntryKeys:  projection.EntryKeys,
			PrimaryKey: proto.Bool(projection.PrimaryKey),
		}
	}

	connectn, err := c.pool.Get()
	if err != nil {
		return err, false
	}
	healthy := true
	closeStream := false
	conn, pkt := connectn.conn, connectn.pkt
	defer func() {
		go func() {
			if closeStream {
				_, healthy = c.closeStream(conn, pkt, requestId)
			}
			c.pool.Return(connectn, healthy)
		}()
	}()

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.ScanRequest{
		DefnID: proto.Uint64(defnID),
		Span: &protobuf.Span{
			Range: nil,
		},
		RequestId:       proto.String(requestId),
		Distinct:        proto.Bool(distinct),
		Limit:           proto.Int64(limit),
		Cons:            proto.Uint32(uint32(cons)),
		Scans:           protoScans,
		Indexprojection: protoProjection,
		Reverse:         proto.Bool(reverse),
		Offset:          proto.Int64(offset),
		RollbackTime:    proto.Int64(rollbackTime),
		PartitionIds:    partnIds,
		Sorted:          proto.Bool(true),
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}
	// ---> protobuf.ScanRequest
	if err := c.sendRequest(conn, pkt, req); err != nil {
		fmsg := "%v Range(%v) request transport failed `%v`\n"
		logging.Errorf(fmsg, c.logPrefix, requestId, err)
		healthy = false
		return err, false
	}

	cont, partial := true, false
	for cont {
		// <--- protobuf.ResponseStream
		cont, healthy, err, closeStream = c.streamResponse(conn, pkt, callb, requestId)
		if err != nil { // if err, cont should have been set to false
			fmsg := "%v Scans(%v) response failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, err)
		} else { // partial succeeded
			partial = true
		}
	}
	return err, partial
}

// CountLookup to count number entries for given set of keys.
func (c *GsiScanClient) CountLookup(
	defnID uint64, requestId string, values []common.SecondaryKey,
	cons common.Consistency, vector *TsConsistency, rollbackTime int64, partitions []common.PartitionId) (int64, error) {

	// serialize match value.
	equals := make([][]byte, 0, len(values))
	for _, value := range values {
		val, err := json.Marshal(value)
		if err != nil {
			return 0, err
		}
		equals = append(equals, val)
	}

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.CountRequest{
		DefnID:       proto.Uint64(defnID),
		RequestId:    proto.String(requestId),
		Span:         &protobuf.Span{Equals: equals},
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}
	resp, err := c.doRequestResponse(req, requestId)
	if err != nil {
		return 0, err
	}
	countResp := resp.(*protobuf.CountResponse)
	if countResp.GetErr() != nil {
		err = errors.New(countResp.GetErr().GetError())
		return 0, err
	}
	return countResp.GetCount(), nil
}

// CountLookup to count number entries for given set of keys for primary index
func (c *GsiScanClient) CountLookupPrimary(
	defnID uint64, requestId string, values [][]byte,
	cons common.Consistency, vector *TsConsistency, rollbackTime int64, partitions []common.PartitionId) (int64, error) {

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.CountRequest{
		DefnID:       proto.Uint64(defnID),
		RequestId:    proto.String(requestId),
		Span:         &protobuf.Span{Equals: values},
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}
	resp, err := c.doRequestResponse(req, requestId)
	if err != nil {
		return 0, err
	}
	countResp := resp.(*protobuf.CountResponse)
	if countResp.GetErr() != nil {
		err = errors.New(countResp.GetErr().GetError())
		return 0, err
	}
	return countResp.GetCount(), nil
}

// CountRange to count number entries in the given range.
func (c *GsiScanClient) CountRange(
	defnID uint64, requestId string, low, high common.SecondaryKey, inclusion Inclusion,
	cons common.Consistency, vector *TsConsistency, rollbackTime int64, partitions []common.PartitionId) (int64, error) {

	// serialize low and high values.
	l, err := json.Marshal(low)
	if err != nil {
		return 0, err
	}
	h, err := json.Marshal(high)
	if err != nil {
		return 0, err
	}

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.CountRequest{
		DefnID:    proto.Uint64(defnID),
		RequestId: proto.String(requestId),
		Span: &protobuf.Span{
			Range: &protobuf.Range{
				Low: l, High: h, Inclusion: proto.Uint32(uint32(inclusion)),
			},
		},
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}

	resp, err := c.doRequestResponse(req, requestId)
	if err != nil {
		return 0, err
	}
	countResp := resp.(*protobuf.CountResponse)
	if countResp.GetErr() != nil {
		err = errors.New(countResp.GetErr().GetError())
		return 0, err
	}
	return countResp.GetCount(), nil
}

// CountRange to count number entries in the given range for primary index
func (c *GsiScanClient) CountRangePrimary(
	defnID uint64, requestId string, low, high []byte, inclusion Inclusion,
	cons common.Consistency, vector *TsConsistency, rollbackTime int64, partitions []common.PartitionId) (int64, error) {

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.CountRequest{
		DefnID:    proto.Uint64(defnID),
		RequestId: proto.String(requestId),
		Span: &protobuf.Span{
			Range: &protobuf.Range{
				Low: low, High: high, Inclusion: proto.Uint32(uint32(inclusion)),
			},
		},
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}

	resp, err := c.doRequestResponse(req, requestId)
	if err != nil {
		return 0, err
	}
	countResp := resp.(*protobuf.CountResponse)
	if countResp.GetErr() != nil {
		err = errors.New(countResp.GetErr().GetError())
		return 0, err
	}
	return countResp.GetCount(), nil
}

func (c *GsiScanClient) MultiScanCount(
	defnID uint64, requestId string, scans Scans, distinct bool,
	cons common.Consistency, vector *TsConsistency, rollbackTime int64, partitions []common.PartitionId) (int64, error) {

	// serialize scans
	protoScans := make([]*protobuf.Scan, len(scans))
	for i, scan := range scans {
		if scan != nil {
			var equals [][]byte
			var filters []*protobuf.CompositeElementFilter

			// If Seek is there, then do not marshall Range
			if len(scan.Seek) > 0 {
				equals = make([][]byte, len(scan.Seek))
				for i, seek := range scan.Seek {
					s, err := json.Marshal(seek)
					if err != nil {
						return 0, err
					}
					equals[i] = s
				}
			} else {
				filters = make([]*protobuf.CompositeElementFilter, len(scan.Filter))
				if scan.Filter != nil {
					for j, f := range scan.Filter {
						var l, h []byte
						var err error
						if f.Low != common.MinUnbounded { // Do not encode if unbounded
							l, err = json.Marshal(f.Low)
							if err != nil {
								return 0, err
							}
						}
						if f.High != common.MaxUnbounded { // Do not encode if unbounded
							h, err = json.Marshal(f.High)
							if err != nil {
								return 0, err
							}
						}

						fl := &protobuf.CompositeElementFilter{
							Low: l, High: h, Inclusion: proto.Uint32(uint32(f.Inclusion)),
						}

						filters[j] = fl
					}
				}
			}
			s := &protobuf.Scan{
				Filters: filters,
				Equals:  equals,
			}
			protoScans[i] = s
		}
	}

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.CountRequest{
		DefnID:    proto.Uint64(defnID),
		RequestId: proto.String(requestId),
		Span: &protobuf.Span{
			Range: nil,
		},
		Distinct:     proto.Bool(distinct),
		Scans:        protoScans,
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
	}

	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}

	resp, err := c.doRequestResponse(req, requestId)
	if err != nil {
		return 0, err
	}
	countResp := resp.(*protobuf.CountResponse)
	if countResp.GetErr() != nil {
		err = errors.New(countResp.GetErr().GetError())
		return 0, err
	}
	return countResp.GetCount(), nil
}

func (c *GsiScanClient) MultiScanCountPrimary(
	defnID uint64, requestId string, scans Scans, distinct bool,
	cons common.Consistency, vector *TsConsistency, rollbackTime int64, partitions []common.PartitionId) (int64, error) {

	var what string
	// serialize scans
	protoScans := make([]*protobuf.Scan, 0)
	for _, scan := range scans {
		if scan != nil {
			var equals [][]byte
			var filters []*protobuf.CompositeElementFilter

			// If Seek is there, then ignore Range
			if len(scan.Seek) > 0 {
				var k []byte
				key := scan.Seek[0]
				if k, what = curePrimaryKey(key); what == "after" {
					continue
				}
				equals = [][]byte{k}
			} else {
				filters = make([]*protobuf.CompositeElementFilter, 0)
				skip := false
				if scan.Filter != nil {
					for _, f := range scan.Filter {
						var l, h []byte
						if f.Low != common.MinUnbounded { // Ignore if unbounded
							if l, what = curePrimaryKey(f.Low); what == "after" {
								skip = true
								break
							}
						}
						if f.High != common.MaxUnbounded { // Ignore if unbounded
							if h, what = curePrimaryKey(f.High); what == "before" {
								skip = true
								break
							}
						}

						fl := &protobuf.CompositeElementFilter{
							Low: l, High: h, Inclusion: proto.Uint32(uint32(f.Inclusion)),
						}

						filters = append(filters, fl)
					}

					if skip {
						continue
					}
				}
			}
			s := &protobuf.Scan{
				Filters: filters,
				Equals:  equals,
			}
			protoScans = append(protoScans, s)
		}
	}

	if len(protoScans) == 0 {
		return 0, nil
	}

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.CountRequest{
		DefnID:    proto.Uint64(defnID),
		RequestId: proto.String(requestId),
		Span: &protobuf.Span{
			Range: nil,
		},
		Distinct:     proto.Bool(distinct),
		Scans:        protoScans,
		Cons:         proto.Uint32(uint32(cons)),
		RollbackTime: proto.Int64(rollbackTime),
		PartitionIds: partnIds,
	}

	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}

	resp, err := c.doRequestResponse(req, requestId)
	if err != nil {
		return 0, err
	}
	countResp := resp.(*protobuf.CountResponse)
	if countResp.GetErr() != nil {
		err = errors.New(countResp.GetErr().GetError())
		return 0, err
	}
	return countResp.GetCount(), nil
}

func (c *GsiScanClient) Scan3(
	defnID uint64, requestId string, scans Scans,
	reverse, distinct bool, projection *IndexProjection, offset, limit int64,
	groupAggr *GroupAggr, sorted bool,
	cons common.Consistency, vector *TsConsistency,
	callb ResponseHandler, rollbackTime int64, partitions []common.PartitionId) (error, bool) {

	// serialize scans
	protoScans := make([]*protobuf.Scan, len(scans))
	for i, scan := range scans {
		if scan != nil {
			var equals [][]byte
			var filters []*protobuf.CompositeElementFilter

			// If Seek is there, then do not marshall Range
			if len(scan.Seek) > 0 {
				equals = make([][]byte, len(scan.Seek))
				for i, seek := range scan.Seek {
					s, err := json.Marshal(seek)
					if err != nil {
						return err, false
					}
					equals[i] = s
				}
			} else {
				filters = make([]*protobuf.CompositeElementFilter, len(scan.Filter))
				if scan.Filter != nil {
					for j, f := range scan.Filter {
						var l, h []byte
						var err error
						if f.Low != common.MinUnbounded { // Do not encode if unbounded
							l, err = json.Marshal(f.Low)
							if err != nil {
								return err, false
							}
						}
						if f.High != common.MaxUnbounded { // Do not encode if unbounded
							h, err = json.Marshal(f.High)
							if err != nil {
								return err, false
							}
						}

						fl := &protobuf.CompositeElementFilter{
							Low: l, High: h, Inclusion: proto.Uint32(uint32(f.Inclusion)),
						}

						filters[j] = fl
					}
				}
			}
			s := &protobuf.Scan{
				Filters: filters,
				Equals:  equals,
			}
			protoScans[i] = s
		}
	}

	//IndexProjection
	var protoProjection *protobuf.IndexProjection
	if projection != nil {
		protoProjection = &protobuf.IndexProjection{
			EntryKeys:  projection.EntryKeys,
			PrimaryKey: proto.Bool(projection.PrimaryKey),
		}
	}

	// Groups and Aggregates
	var protoGroupAggr *protobuf.GroupAggr
	if groupAggr != nil {
		// GroupKeys
		protoGroupKeys := make([]*protobuf.GroupKey, len(groupAggr.Group))
		for i, grp := range groupAggr.Group {
			gk := &protobuf.GroupKey{
				EntryKeyId: proto.Int32(grp.EntryKeyId),
				KeyPos:     proto.Int32(grp.KeyPos),
				Expr:       []byte(grp.Expr),
			}
			protoGroupKeys[i] = gk
		}
		// Aggregates
		protoAggregates := make([]*protobuf.Aggregate, len(groupAggr.Aggrs))
		for i, aggr := range groupAggr.Aggrs {
			ag := &protobuf.Aggregate{
				AggrFunc:   proto.Uint32(uint32(aggr.AggrFunc)),
				EntryKeyId: proto.Int32(aggr.EntryKeyId),
				KeyPos:     proto.Int32(aggr.KeyPos),
				Expr:       []byte(aggr.Expr),
				Distinct:   proto.Bool(aggr.Distinct),
			}
			protoAggregates[i] = ag
		}
		protoIndexKeyNames := make([][]byte, len(groupAggr.IndexKeyNames))
		for i, keyName := range groupAggr.IndexKeyNames {
			protoIndexKeyNames[i] = []byte(keyName)
		}
		protoGroupAggr = &protobuf.GroupAggr{
			Name:               []byte(groupAggr.Name),
			GroupKeys:          protoGroupKeys,
			Aggrs:              protoAggregates,
			DependsOnIndexKeys: groupAggr.DependsOnIndexKeys,
			IndexKeyNames:      protoIndexKeyNames,
			AllowPartialAggr:   proto.Bool(groupAggr.AllowPartialAggr),
		}
	}

	connectn, err := c.pool.Get()
	if err != nil {
		return err, false
	}
	healthy := true
	closeStream := false
	conn, pkt := connectn.conn, connectn.pkt
	defer func() {
		go func() {
			if closeStream {
				_, healthy = c.closeStream(conn, pkt, requestId)
			}
			c.pool.Return(connectn, healthy)
		}()
	}()

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.ScanRequest{
		DefnID: proto.Uint64(defnID),
		Span: &protobuf.Span{
			Range: nil,
		},
		RequestId:       proto.String(requestId),
		Distinct:        proto.Bool(distinct),
		Limit:           proto.Int64(limit),
		Cons:            proto.Uint32(uint32(cons)),
		Scans:           protoScans,
		Indexprojection: protoProjection,
		Reverse:         proto.Bool(reverse),
		Offset:          proto.Int64(offset),
		RollbackTime:    proto.Int64(rollbackTime),
		PartitionIds:    partnIds,
		GroupAggr:       protoGroupAggr,
		Sorted:          proto.Bool(sorted),
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}
	// ---> protobuf.ScanRequest
	if err := c.sendRequest(conn, pkt, req); err != nil {
		fmsg := "%v Range(%v) request transport failed `%v`\n"
		logging.Errorf(fmsg, c.logPrefix, requestId, err)
		healthy = false
		return err, false
	}

	cont, partial := true, false
	for cont {
		// <--- protobuf.ResponseStream
		cont, healthy, err, closeStream = c.streamResponse(conn, pkt, callb, requestId)
		if err != nil { // if err, cont should have been set to false
			fmsg := "%v Scans(%v) response failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, err)
		} else { // partial succeeded
			partial = true
		}
	}
	return err, partial
}

func (c *GsiScanClient) Scan3Primary(
	defnID uint64, requestId string, scans Scans,
	reverse, distinct bool, projection *IndexProjection, offset, limit int64,
	groupAggr *GroupAggr, sorted bool,
	cons common.Consistency, vector *TsConsistency,
	callb ResponseHandler, rollbackTime int64, partitions []common.PartitionId) (error, bool) {

	var what string
	// serialize scans
	protoScans := make([]*protobuf.Scan, 0)
	for _, scan := range scans {
		if scan != nil {
			var equals [][]byte
			var filters []*protobuf.CompositeElementFilter

			// If Seek is there, then ignore Range
			if len(scan.Seek) > 0 {
				var k []byte
				key := scan.Seek[0]
				if k, what = curePrimaryKey(key); what == "after" {
					continue
				}
				equals = [][]byte{k}
			} else {
				filters = make([]*protobuf.CompositeElementFilter, 0)
				skip := false
				if scan.Filter != nil {
					for _, f := range scan.Filter {
						var l, h []byte
						if f.Low != common.MinUnbounded { // Ignore if unbounded
							if l, what = curePrimaryKey(f.Low); what == "after" {
								skip = true
								break
							}
						}
						if f.High != common.MaxUnbounded { // Ignore if unbounded
							if h, what = curePrimaryKey(f.High); what == "before" {
								skip = true
								break
							}
						}

						fl := &protobuf.CompositeElementFilter{
							Low: l, High: h, Inclusion: proto.Uint32(uint32(f.Inclusion)),
						}

						filters = append(filters, fl)
					}
					if skip {
						continue
					}
				}
			}
			s := &protobuf.Scan{
				Filters: filters,
				Equals:  equals,
			}
			protoScans = append(protoScans, s)
		}
	}

	if len(protoScans) == 0 {
		protoScans = append(protoScans, getEmptySpanForPrimary())
	}

	//IndexProjection
	var protoProjection *protobuf.IndexProjection
	if projection != nil {
		protoProjection = &protobuf.IndexProjection{
			EntryKeys:  projection.EntryKeys,
			PrimaryKey: proto.Bool(projection.PrimaryKey),
		}
	}

	// Groups and Aggregates
	var protoGroupAggr *protobuf.GroupAggr
	if groupAggr != nil {
		// GroupKeys
		protoGroupKeys := make([]*protobuf.GroupKey, len(groupAggr.Group))
		for i, grp := range groupAggr.Group {
			gk := &protobuf.GroupKey{
				EntryKeyId: proto.Int32(grp.EntryKeyId),
				KeyPos:     proto.Int32(grp.KeyPos),
				Expr:       []byte(grp.Expr),
			}
			protoGroupKeys[i] = gk
		}
		// Aggregates
		protoAggregates := make([]*protobuf.Aggregate, len(groupAggr.Aggrs))
		for i, aggr := range groupAggr.Aggrs {
			ag := &protobuf.Aggregate{
				AggrFunc:   proto.Uint32(uint32(aggr.AggrFunc)),
				EntryKeyId: proto.Int32(aggr.EntryKeyId),
				KeyPos:     proto.Int32(aggr.KeyPos),
				Expr:       []byte(aggr.Expr),
				Distinct:   proto.Bool(aggr.Distinct),
			}
			protoAggregates[i] = ag
		}
		protoIndexKeyNames := make([][]byte, len(groupAggr.IndexKeyNames))
		for i, keyName := range groupAggr.IndexKeyNames {
			protoIndexKeyNames[i] = []byte(keyName)
		}
		protoGroupAggr = &protobuf.GroupAggr{
			Name:               []byte(groupAggr.Name),
			GroupKeys:          protoGroupKeys,
			Aggrs:              protoAggregates,
			DependsOnIndexKeys: groupAggr.DependsOnIndexKeys,
			IndexKeyNames:      protoIndexKeyNames,
		}
	}

	connectn, err := c.pool.Get()
	if err != nil {
		return err, false
	}
	healthy := true
	closeStream := false
	conn, pkt := connectn.conn, connectn.pkt
	defer func() {
		go func() {
			if closeStream {
				_, healthy = c.closeStream(conn, pkt, requestId)
			}
			c.pool.Return(connectn, healthy)
		}()
	}()

	partnIds := make([]uint64, len(partitions))
	for i, partnId := range partitions {
		partnIds[i] = uint64(partnId)
	}

	req := &protobuf.ScanRequest{
		DefnID: proto.Uint64(defnID),
		Span: &protobuf.Span{
			Range: nil,
		},
		RequestId:       proto.String(requestId),
		Distinct:        proto.Bool(distinct),
		Limit:           proto.Int64(limit),
		Cons:            proto.Uint32(uint32(cons)),
		Scans:           protoScans,
		Indexprojection: protoProjection,
		Reverse:         proto.Bool(reverse),
		Offset:          proto.Int64(offset),
		RollbackTime:    proto.Int64(rollbackTime),
		PartitionIds:    partnIds,
		GroupAggr:       protoGroupAggr,
		Sorted:          proto.Bool(sorted),
	}
	if vector != nil {
		req.Vector = protobuf.NewTsConsistency(
			vector.Vbnos, vector.Seqnos, vector.Vbuuids, vector.Crc64)
	}
	// ---> protobuf.ScanRequest
	if err := c.sendRequest(conn, pkt, req); err != nil {
		fmsg := "%v Range(%v) request transport failed `%v`\n"
		logging.Errorf(fmsg, c.logPrefix, requestId, err)
		healthy = false
		return err, false
	}

	cont, partial := true, false
	for cont {
		// <--- protobuf.ResponseStream
		cont, healthy, err, closeStream = c.streamResponse(conn, pkt, callb, requestId)
		if err != nil { // if err, cont should have been set to false
			fmsg := "%v Scans(%v) response failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, err)
		} else { // partial succeeded
			partial = true
		}
	}
	return err, partial
}

func (c *GsiScanClient) Close() error {
	return c.pool.Close()
}

func (c *GsiScanClient) doRequestResponse(
	req interface{}, requestId string) (interface{}, error) {

	connectn, err := c.pool.Get()
	if err != nil {
		return nil, err
	}
	healthy := true
	defer func() { c.pool.Return(connectn, healthy) }()

	conn, pkt := connectn.conn, connectn.pkt

	// ---> protobuf.*Request
	if err := c.sendRequest(conn, pkt, req); err != nil {
		fmsg := "%v %T(%v) request transport failed `%v`\n"
		arg1 := logging.TagUD(req)
		logging.Errorf(fmsg, c.logPrefix, arg1, requestId, err)
		healthy = false
		return nil, err
	}

	laddr := conn.LocalAddr()
	c.trySetDeadline(conn, c.readDeadline)
	// <--- protobuf.*Response
	resp, err := pkt.Receive(conn)
	if err != nil {
		fmsg := "%v req(%v) connection %v response %T transport failed `%v`\n"
		arg1 := logging.TagUD(req)
		logging.Errorf(fmsg, c.logPrefix, requestId, laddr, arg1, err)
		healthy = false
		return nil, err
	}

	c.trySetDeadline(conn, c.readDeadline)
	// <--- protobuf.StreamEndResponse (skipped) TODO: knock this off.
	if endResp, err := pkt.Receive(conn); err != nil {
		fmsg := "%v req(%v) connection %v response %T transport failed `%v`\n"
		arg1 := logging.TagUD(req)
		logging.Errorf(fmsg, c.logPrefix, requestId, laddr, arg1, err)
		healthy = false
		return nil, err
	} else if endResp != nil {
		healthy = false
		return nil, ErrorProtocol
	}
	return resp, nil
}

func (c *GsiScanClient) sendRequest(
	conn net.Conn, pkt *transport.TransportPacket, req interface{}) (err error) {

	c.trySetDeadline(conn, c.writeDeadline)
	return pkt.Send(conn, req)
}

func (c *GsiScanClient) streamResponse(
	conn net.Conn,
	pkt *transport.TransportPacket,
	callb ResponseHandler, requestId string) (cont bool, healthy bool, err error, closeStream bool) {

	var resp interface{}
	var finish bool

	closeStream = false
	laddr := conn.LocalAddr()
	c.trySetDeadline(conn, c.readDeadline)
	if resp, err = pkt.Receive(conn); err != nil {
		//resp := &protobuf.ResponseStream{
		//    Err: &protobuf.Error{Error: proto.String(err.Error())},
		//}
		//callb(resp) // callback with error
		cont, healthy = false, false
		if err == io.EOF {
			fmsg := "%v req(%v) connection %q closed `%v` \n"
			logging.Errorf(fmsg, c.logPrefix, requestId, laddr, err)
		} else {
			fmsg := "%v req(%v) connection %q response transport failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, laddr, err)
		}

	} else if resp == nil {
		finish = true
		fmsg := "%v req(%v) connection %q received StreamEndResponse"
		logging.Tracef(fmsg, c.logPrefix, requestId, laddr)
		callb(&protobuf.StreamEndResponse{}) // callback most likely return true
		cont, healthy = false, true

	} else {
		streamResp := resp.(*protobuf.ResponseStream)
		if err = streamResp.Error(); err == nil {
			cont = callb(streamResp)
		}
		healthy = true
	}

	if cont == false && healthy == true && finish == false {
		closeStream = true
	}
	return
}

func (c *GsiScanClient) closeStream(
	conn net.Conn, pkt *transport.TransportPacket,
	requestId string) (err error, healthy bool) {

	var resp interface{}
	laddr := conn.LocalAddr()
	healthy = true
	// request server to end the stream.
	err = c.sendRequest(conn, pkt, &protobuf.EndStreamRequest{})
	if err != nil {
		fmsg := "%v closeStream(%v) request transport failed `%v`\n"
		logging.Errorf(fmsg, c.logPrefix, requestId, err)
		healthy = false
		return
	}
	fmsg := "%v req(%v) connection %q transmitted protobuf.EndStreamRequest"
	logging.Tracef(fmsg, c.logPrefix, requestId, laddr)

	// flush the connection until stream has ended.
	for true {
		c.trySetDeadline(conn, c.readDeadline)
		resp, err = pkt.Receive(conn)
		if err != nil {
			healthy = false
			if err == io.EOF {
				fmsg := "%v req(%v) connection %q closed `%v`\n"
				logging.Errorf(fmsg, c.logPrefix, requestId, laddr, err)
				return
			}
			fmsg := "%v req(%v) connection %q response transport failed `%v`\n"
			logging.Errorf(fmsg, c.logPrefix, requestId, laddr, err)
			return

		} else if resp == nil { // End of stream marker
			return
		}
	}
	return
}

func (c *GsiScanClient) trySetDeadline(conn net.Conn, deadline time.Duration) {
	if deadline > time.Duration(0) {
		timeoutMs := deadline * time.Millisecond
		conn.SetReadDeadline(time.Now().Add(timeoutMs))
	}
}

func getEmptySpanForPrimary() *protobuf.Scan {
	fl := &protobuf.CompositeElementFilter{
		Low: []byte(""), High: []byte(""), Inclusion: proto.Uint32(uint32(0)),
	}
	return &protobuf.Scan{Filters: []*protobuf.CompositeElementFilter{fl}}
}
