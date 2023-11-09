package main

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/ybbus/jsonrpc/v3"
)

type Config struct {
	Port            int      `yaml:"port"`
	MasterClientWSS string   `yaml:"masterClientWSS"`
	ChildClientsRPC []string `yaml:"childClientsRPC"`
	ContractToSync  string   `yaml:"contractToSync"`
}

// SyncContract RPC Struct
type SyncContract struct {
	db                      *leveldb.DB
	masterClient            *ethclient.Client
	childClients            []jsonrpc.RPCClient
	ContractToSync          common.Address
	MasterClientEndpointURL string
	ChildClientsEndpointURL []string
}

type GetContractToSyncArgs struct{}

type GetContractToSyncReply struct {
	ContractToSync common.Address
}

type GetClientsDataArgs struct{}

type GetClientsDataReply struct {
	MasterClient string
	ChildClients []string
}

type GetSlotDataArgs struct {
	SlotNum common.Hash
}

type GetSlotDataReply struct {
	SlotData []SlotData
}

type AddChildClientArgs struct {
	ChildClientEndpointURL string
}

type AddChildClientReply struct {
}

type RemoveChildClientArgs struct {
	ChildClientEndpointURL string
}

type RemoveChildClientReply struct {
}

type SlotData struct {
	Data                  common.Hash
	TimeStamp             int64
	TransactionNumInBlock int
}

type SlotDataList []SlotData

func (slotData SlotDataList) Len() int {
	return len(slotData)
}

func (slotData SlotDataList) Less(i, j int) bool {
	if slotData[i].TimeStamp == slotData[j].TimeStamp {
		return slotData[i].TransactionNumInBlock < slotData[j].TransactionNumInBlock
	} else if slotData[i].TimeStamp < slotData[j].TimeStamp {
		return true
	}
	return false
}

func (slotData SlotDataList) Swap(i, j int) {
	temp := slotData[i]
	slotData[i] = slotData[j]
	slotData[j] = temp
}
