package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	logger "github.com/inconshreveable/log15"
	"github.com/ybbus/jsonrpc/v3"
)

func (syncContract *SyncContract) GetContractToSync(r *http.Request, req *GetContractToSyncArgs, res *GetContractToSyncReply) error {
	logger.Info("Received `GetContractToSync` Request", "RemoteAddr", r.RemoteAddr)
	res.ContractToSync = syncContract.ContractToSync
	return nil
}

func (syncContract *SyncContract) GetClientsData(r *http.Request, req *GetClientsDataArgs, res *GetClientsDataReply) error {
	logger.Info("Received `GetClientsData` Request", "RemoteAddr", r.RemoteAddr)
	res.MasterClient = syncContract.MasterClientEndpointURL
	res.ChildClients = syncContract.ChildClientsEndpointURL
	return nil
}

func (syncContract *SyncContract) GetSlotData(r *http.Request, req *GetSlotDataArgs, res *GetSlotDataReply) error {
	logger.Info("Received `GetSlotData` Request", "RemoteAddr", r.RemoteAddr)
	hasKey, err := syncContract.db.Has(req.SlotNum.Bytes(), nil)
	if err != nil {
		logger.Error("Failed To Check Whether Key Exists", "Error", err)
		return fmt.Errorf("failed to check whether key exists: %v", err)
	}
	if !hasKey {
		return fmt.Errorf("SlotData Not Exists")
	}
	data, err := syncContract.db.Get(req.SlotNum.Bytes(), nil)
	if err != nil {
		logger.Error("Failed To Get SlotData", "RemoteAddr", r.RemoteAddr, "Error: ", err)
		return fmt.Errorf("failed to get slot data, may be slot data not exist: %v", err)
	}
	slotData := []SlotData{}
	if err := json.Unmarshal(data, &slotData); err != nil {
		logger.Error("Failed To Unmarshal Slot Data", "Error", err)
		return fmt.Errorf("failed to unmarshal slot data: %v", err)
	}
	res.SlotData = slotData
	return nil
}

func (syncContract *SyncContract) AddChildClient(r *http.Request, req *AddChildClientArgs, res *AddChildClientReply) error {
	logger.Info("Received `AddChildClient` Request", "RemoteAddr", r.RemoteAddr)
	for _, childClientEndpointURL := range syncContract.ChildClientsEndpointURL {
		if childClientEndpointURL == req.ChildClientEndpointURL {
			logger.Error("Passed Child Client Already Exists", "PassedChildClientEndpointURL", req.ChildClientEndpointURL)
			return fmt.Errorf("passed child client already exists")
		}
	}
	client := jsonrpc.NewClient(req.ChildClientEndpointURL)
	_, err := client.Call(context.Background(), "eth_chainId")
	if err != nil {
		logger.Error("Failed To Connect To Client", "Error", err)
		return fmt.Errorf("failed to connect to client: %v", err)
	}
	syncContract.childClients = append(syncContract.childClients, client)
	syncContract.ChildClientsEndpointURL = append(syncContract.ChildClientsEndpointURL, req.ChildClientEndpointURL)
	logger.Info("New Child Client Added", "AddedChildClient", req.ChildClientEndpointURL)
	return nil
}

func (syncContract *SyncContract) RemoveChildClient(r *http.Request, req *RemoveChildClientArgs, res *RemoveChildClientReply) error {
	logger.Info("Received `RemoveChildClient` Request", "RemoteAddr", r.RemoteAddr)
	for i, childClientEndpointURL := range syncContract.ChildClientsEndpointURL {
		if childClientEndpointURL == req.ChildClientEndpointURL {
			syncContract.childClients = append(syncContract.childClients[:i], syncContract.childClients[i+1:]...)
			syncContract.ChildClientsEndpointURL = append(syncContract.ChildClientsEndpointURL[:i], syncContract.ChildClientsEndpointURL[i+1:]...)
			logger.Info("Child Client Removed", "RemovedChildClient", req.ChildClientEndpointURL)
			return nil
		}
	}
	return nil
}
