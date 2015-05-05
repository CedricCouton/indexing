// Code generated by protoc-gen-go.
// source: index.proto
// DO NOT EDIT!

package protobuf

import proto "github.com/golang/protobuf/proto"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = math.Inf

// IndexDefn will be in one of the following state
type IndexState int32

const (
	// Create index accepted, replicated and response sent back to admin
	// console.
	IndexState_IndexInitial IndexState = 1
	// Index DDL replicated, and then communicated to participating indexers.
	IndexState_IndexPending IndexState = 2
	// Initial-load request received from admin console, DDL replicated,
	// loading status communicated with participating indexer and
	// initial-load request is posted to projector.
	IndexState_IndexLoading IndexState = 3
	// Initial-loading is completed for this index from all partiticipating
	// indexers, DDL replicated, and finaly initial-load stream is shutdown.
	IndexState_IndexActive IndexState = 4
	// Delete index request is received, replicated and then communicated with
	// each participating indexer nodes.
	IndexState_IndexDeleted IndexState = 5
)

var IndexState_name = map[int32]string{
	1: "IndexInitial",
	2: "IndexPending",
	3: "IndexLoading",
	4: "IndexActive",
	5: "IndexDeleted",
}
var IndexState_value = map[string]int32{
	"IndexInitial": 1,
	"IndexPending": 2,
	"IndexLoading": 3,
	"IndexActive":  4,
	"IndexDeleted": 5,
}

func (x IndexState) Enum() *IndexState {
	p := new(IndexState)
	*p = x
	return p
}
func (x IndexState) String() string {
	return proto.EnumName(IndexState_name, int32(x))
}
func (x *IndexState) UnmarshalJSON(data []byte) error {
	value, err := proto.UnmarshalJSONEnum(IndexState_value, data, "IndexState")
	if err != nil {
		return err
	}
	*x = IndexState(value)
	return nil
}

// List of possible index storage algorithms.
type StorageType int32

const (
	StorageType_View     StorageType = 1
	StorageType_Llrb     StorageType = 2
	StorageType_LevelDB  StorageType = 3
	StorageType_ForestDB StorageType = 4
)

var StorageType_name = map[int32]string{
	1: "View",
	2: "Llrb",
	3: "LevelDB",
	4: "ForestDB",
}
var StorageType_value = map[string]int32{
	"View":     1,
	"Llrb":     2,
	"LevelDB":  3,
	"ForestDB": 4,
}

func (x StorageType) Enum() *StorageType {
	p := new(StorageType)
	*p = x
	return p
}
func (x StorageType) String() string {
	return proto.EnumName(StorageType_name, int32(x))
}
func (x *StorageType) UnmarshalJSON(data []byte) error {
	value, err := proto.UnmarshalJSONEnum(StorageType_value, data, "StorageType")
	if err != nil {
		return err
	}
	*x = StorageType(value)
	return nil
}

// Type of expression used to evaluate document.
type ExprType int32

const (
	ExprType_JavaScript ExprType = 1
	ExprType_N1QL       ExprType = 2
)

var ExprType_name = map[int32]string{
	1: "JavaScript",
	2: "N1QL",
}
var ExprType_value = map[string]int32{
	"JavaScript": 1,
	"N1QL":       2,
}

func (x ExprType) Enum() *ExprType {
	p := new(ExprType)
	*p = x
	return p
}
func (x ExprType) String() string {
	return proto.EnumName(ExprType_name, int32(x))
}
func (x *ExprType) UnmarshalJSON(data []byte) error {
	value, err := proto.UnmarshalJSONEnum(ExprType_value, data, "ExprType")
	if err != nil {
		return err
	}
	*x = ExprType(value)
	return nil
}

// Type of topology, including paritition type to be used for the index.
type PartitionScheme int32

const (
	PartitionScheme_TEST   PartitionScheme = 1
	PartitionScheme_SINGLE PartitionScheme = 2
	PartitionScheme_KEY    PartitionScheme = 3
	PartitionScheme_HASH   PartitionScheme = 4
	PartitionScheme_RANGE  PartitionScheme = 5
)

var PartitionScheme_name = map[int32]string{
	1: "TEST",
	2: "SINGLE",
	3: "KEY",
	4: "HASH",
	5: "RANGE",
}
var PartitionScheme_value = map[string]int32{
	"TEST":   1,
	"SINGLE": 2,
	"KEY":    3,
	"HASH":   4,
	"RANGE":  5,
}

func (x PartitionScheme) Enum() *PartitionScheme {
	p := new(PartitionScheme)
	*p = x
	return p
}
func (x PartitionScheme) String() string {
	return proto.EnumName(PartitionScheme_name, int32(x))
}
func (x *PartitionScheme) UnmarshalJSON(data []byte) error {
	value, err := proto.UnmarshalJSONEnum(PartitionScheme_value, data, "PartitionScheme")
	if err != nil {
		return err
	}
	*x = PartitionScheme(value)
	return nil
}

// IndexInst message as payload between co-ordinator, projector, indexer.
type IndexInst struct {
	InstId           *uint64          `protobuf:"varint,1,req,name=instId" json:"instId,omitempty"`
	State            *IndexState      `protobuf:"varint,2,req,name=state,enum=protobuf.IndexState" json:"state,omitempty"`
	Definition       *IndexDefn       `protobuf:"bytes,3,req,name=definition" json:"definition,omitempty"`
	Tp               *TestPartition   `protobuf:"bytes,4,opt,name=tp" json:"tp,omitempty"`
	SinglePartn      *SinglePartition `protobuf:"bytes,5,opt,name=singlePartn" json:"singlePartn,omitempty"`
	XXX_unrecognized []byte           `json:"-"`
}

func (m *IndexInst) Reset()         { *m = IndexInst{} }
func (m *IndexInst) String() string { return proto.CompactTextString(m) }
func (*IndexInst) ProtoMessage()    {}

func (m *IndexInst) GetInstId() uint64 {
	if m != nil && m.InstId != nil {
		return *m.InstId
	}
	return 0
}

func (m *IndexInst) GetState() IndexState {
	if m != nil && m.State != nil {
		return *m.State
	}
	return IndexState_IndexInitial
}

func (m *IndexInst) GetDefinition() *IndexDefn {
	if m != nil {
		return m.Definition
	}
	return nil
}

func (m *IndexInst) GetTp() *TestPartition {
	if m != nil {
		return m.Tp
	}
	return nil
}

func (m *IndexInst) GetSinglePartn() *SinglePartition {
	if m != nil {
		return m.SinglePartn
	}
	return nil
}

// Index DDL from create index statement.
type IndexDefn struct {
	DefnID           *uint64          `protobuf:"varint,1,req,name=defnID" json:"defnID,omitempty"`
	Bucket           *string          `protobuf:"bytes,2,req,name=bucket" json:"bucket,omitempty"`
	IsPrimary        *bool            `protobuf:"varint,3,req,name=isPrimary" json:"isPrimary,omitempty"`
	Name             *string          `protobuf:"bytes,4,req,name=name" json:"name,omitempty"`
	Using            *StorageType     `protobuf:"varint,5,req,name=using,enum=protobuf.StorageType" json:"using,omitempty"`
	ExprType         *ExprType        `protobuf:"varint,6,req,name=exprType,enum=protobuf.ExprType" json:"exprType,omitempty"`
	SecExpressions   []string         `protobuf:"bytes,7,rep,name=secExpressions" json:"secExpressions,omitempty"`
	PartitionScheme  *PartitionScheme `protobuf:"varint,8,opt,name=partitionScheme,enum=protobuf.PartitionScheme" json:"partitionScheme,omitempty"`
	PartnExpression  *string          `protobuf:"bytes,9,opt,name=partnExpression" json:"partnExpression,omitempty"`
	WhereExpression  *string          `protobuf:"bytes,10,opt,name=whereExpression" json:"whereExpression,omitempty"`
	XXX_unrecognized []byte           `json:"-"`
}

func (m *IndexDefn) Reset()         { *m = IndexDefn{} }
func (m *IndexDefn) String() string { return proto.CompactTextString(m) }
func (*IndexDefn) ProtoMessage()    {}

func (m *IndexDefn) GetDefnID() uint64 {
	if m != nil && m.DefnID != nil {
		return *m.DefnID
	}
	return 0
}

func (m *IndexDefn) GetBucket() string {
	if m != nil && m.Bucket != nil {
		return *m.Bucket
	}
	return ""
}

func (m *IndexDefn) GetIsPrimary() bool {
	if m != nil && m.IsPrimary != nil {
		return *m.IsPrimary
	}
	return false
}

func (m *IndexDefn) GetName() string {
	if m != nil && m.Name != nil {
		return *m.Name
	}
	return ""
}

func (m *IndexDefn) GetUsing() StorageType {
	if m != nil && m.Using != nil {
		return *m.Using
	}
	return StorageType_View
}

func (m *IndexDefn) GetExprType() ExprType {
	if m != nil && m.ExprType != nil {
		return *m.ExprType
	}
	return ExprType_JavaScript
}

func (m *IndexDefn) GetSecExpressions() []string {
	if m != nil {
		return m.SecExpressions
	}
	return nil
}

func (m *IndexDefn) GetPartitionScheme() PartitionScheme {
	if m != nil && m.PartitionScheme != nil {
		return *m.PartitionScheme
	}
	return PartitionScheme_TEST
}

func (m *IndexDefn) GetPartnExpression() string {
	if m != nil && m.PartnExpression != nil {
		return *m.PartnExpression
	}
	return ""
}

func (m *IndexDefn) GetWhereExpression() string {
	if m != nil && m.WhereExpression != nil {
		return *m.WhereExpression
	}
	return ""
}

func init() {
	proto.RegisterEnum("protobuf.IndexState", IndexState_name, IndexState_value)
	proto.RegisterEnum("protobuf.StorageType", StorageType_name, StorageType_value)
	proto.RegisterEnum("protobuf.ExprType", ExprType_name, ExprType_value)
	proto.RegisterEnum("protobuf.PartitionScheme", PartitionScheme_name, PartitionScheme_value)
}
