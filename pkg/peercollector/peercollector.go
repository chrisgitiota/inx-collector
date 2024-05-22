package peercollector

import (
	"collector/pkg/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"errors"
	"context"
	"time"
	"bytes"
	"strings"

	"github.com/iotaledger/hive.go/core/logger"
)

type PeerCollectorHandler struct {
	*logger.WrappedLogger
	APIUrl 	string
	Storage storage.Storage
}

func NewPeerCollectorHandler(params Parameters, storage storage.Storage, log *logger.WrappedLogger) PeerCollectorHandler {
	return PeerCollectorHandler{
		WrappedLogger:  logger.NewWrappedLogger(log.LoggerNamed("PeerCollector")),
		APIUrl: params.HostUrl,
		Storage: storage,
	}
}

func (pc *PeerCollectorHandler) SendObjectNameToPeerCollectorForSynchronization(objectName string, logLabel string) (bool, error) {
	bodyTemplate := `{"blockId": "%s"}`
	body := bytes.NewBufferString(fmt.Sprintf(bodyTemplate, objectName))
	resp, err := http.Post(pc.APIUrl + "/synchronized-block", "application/json", body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("%s - POST synchronized-block '%v' to PeerCollector failed, status: %s",
			logLabel,
			objectName,
			resp.Status,
		)
		return false, err
	}
	pc.WrappedLogger.LogInfof("%s - POST synchronized-block '%v' to PeerCollector was SUCCESSFUL", logLabel, objectName)
	return true, nil
}

func (pc *PeerCollectorHandler) IsPeerCollectorUsed() (bool, error) {
	if len(pc.APIUrl) > 0 {
		url, err := url.Parse(pc.APIUrl)
		if err != nil {
			return false, err
		}
		if ! (strings.HasPrefix(pc.APIUrl, "http") || strings.HasPrefix(pc.APIUrl, "https")) {
			return false, errors.New("peercollector parameter 'hostUrl' must start with scheme 'http://' or 'https://'")
		}
		if len(url.Path) > 0 {
			return false, errors.New("peercollector parameter APIUrl must not contain a path")
		}
		return true, nil
	}
	return false, nil
}

func (pc *PeerCollectorHandler) GetObjectFromPeerCollector(objectName string) (storage.Object, error) {
	var object storage.Object
	resp, err := http.Get(pc.APIUrl + "/block/" + objectName)
	if err != nil {
		return object, err
	}
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("GetBlock request from PeerCollector failed, status: %s", resp.Status)
		return object, err
	}
	err = json.NewDecoder(resp.Body).Decode(&object)
	if err != nil {
		return object, err
	}

	return object, nil
}

func (pc *PeerCollectorHandler) ValidateLocalObjectsInObjectsInspectionList(ctx context.Context) {
	objectsInspectionListCh := pc.Storage.GetObjectsInspectionList(ctx)
	keysToRemoveFromInspectionList := []string{}
	inspectionListLength := 0
	numObjectsSyncedFromPeerCollector := 0
	for objectName := range objectsInspectionListCh {
		if objectName.Err != nil {
			pc.WrappedLogger.LogErrorf("Fetching objectName '%s' from ObjectsInspectionList failed. Continuing ObjectsInspection loop . Error (%s)", objectName.Err)
			continue
		}
		inspectionListLength += 1
		_, err := pc.Storage.GetObjectTagging(pc.Storage.DefaultBucketName, objectName.Key, ctx)
		if err != nil {
			pc.WrappedLogger.LogErrorf("GetObjectTagging for object '%s' failed. Copying object from PeerCollector . Error (%s)", objectName, err)
			obj, err := pc.GetObjectFromPeerCollector(objectName.Key)
			if err != nil {
				pc.WrappedLogger.LogErrorf("GetObjectFromPeerCollector for object '%s' failed. Continuing ObjectsInspection loop . Error (%s)", objectName, err)
				continue
			}
			err = pc.Storage.UploadObject(objectName.Key, pc.Storage.DefaultBucketName, obj, ctx)
			if err != nil {
				pc.WrappedLogger.LogErrorf("UploadObject for object '%s' failed. Continuing ObjectsInspection loop . Error (%s)", objectName, err)
				continue
			}
			numObjectsSyncedFromPeerCollector += 1
			// Please note:
			// * objectName.Key is not appended to keysToRemoveFromInspectionList here, so it will remain in the ObjectsInspectionList
			// 	 until the next ValidateLocalObjectsInObjectsInspectionList() run.
			// 	 When ValidateLocalObjectsInObjectsInspectionList() is run the next time the GetObjectTagging call will
			// 	 be successful and the objectName.Key will be removed from the ObjectsInspectionList.
			// * In case of any error (GetObjectFromPeerCollector, UploadObject) the loop is continued.
			//   This means during the next SynchronizePeerCollector run, there will be another attempt
			//   to synchronize the object. This will be repeated until object synchronization is successful.
			//   Currently there is no maximumTrials threshold.
		} else {
			keysToRemoveFromInspectionList = append(keysToRemoveFromInspectionList, objectName.Key)
		}
	}
	pc.WrappedLogger.LogInfof(`ObjectsInspection Results:
- ObjectsInspectionList Length: %v
- Number of objects synced from PeerCollector: %v
- Number of keys removed from ObjectsInspectionList: %v`,
		inspectionListLength,
		numObjectsSyncedFromPeerCollector,
		len(keysToRemoveFromInspectionList),
	)
	for _, objectName := range keysToRemoveFromInspectionList {
		err := pc.Storage.DeleteObjectNameFromObjectsInspectionList(objectName, ctx)
		if err != nil {
			pc.WrappedLogger.LogErrorf("DeleteObjectNameFromObjectsInspectionList for ObjectName '%s' failed. Continuing keysToRemoveFromInspectionList loop", objectName, err)
			// Ignoring this error means that the ObjectName remains in the ObjectsInspectionList
			// and will be inspected again when SynchronizePeerCollector runs the next time.
			// As ObjectsInspection is not very expensive, we do not have any error handling (like
			// additional retrials) here.
		} else {
			pc.WrappedLogger.LogInfof("Object '%s' has been successfully validated and removed from ObjectsInspectionList", objectName)
		}
	}
}

func (pc *PeerCollectorHandler) SendKeysToPeerCollector(ctx context.Context) {
	keysToSend := pc.Storage.GetKeysToBeSendToPeerCollector(ctx)
	keysToSendLength := 0
	numberOfKeysSuccessfullySend := 0
	for key := range keysToSend {
		if key.Err != nil {
			pc.WrappedLogger.LogErrorf("Fetching key '%s' ToBeSendToPeerCollector from minio failed. Continuing SendKeysToPeerCollector loop . Error (%s)", key.Err)
			continue
		}
		keysToSendLength += 1
		success, err := pc.SendObjectNameToPeerCollectorForSynchronization(key.Key, "SendKeysToPeerCollector")
		if err != nil {
			pc.WrappedLogger.LogErrorf("SendObjectNameToPeerCollectorForSynchronization for key '%s' failed. Continuing SendKeysToPeerCollector loop . Error (%s)", key.Key, err)
			continue
		}
		if success != true {
			pc.WrappedLogger.LogErrorf("SendObjectNameToPeerCollectorForSynchronization for key '%s' was not successful. Continuing SendKeysToPeerCollector loop", key.Key)
			continue
		}
		err = pc.Storage.DeleteObjectNameFromKeysToSendToPeerCollectorList(key.Key, ctx)
		if err != nil {
			pc.WrappedLogger.LogErrorf("DeleteObjectNameFromKeysToSendToPeerCollectorList for key '%s' failed. Continuing SendKeysToPeerCollector loop . Error (%s)", key.Key, err)
			continue
		}
		numberOfKeysSuccessfullySend += 1
	}
	pc.WrappedLogger.LogInfof("%v keys, out of %v total keys, have been successfully send to PeerCollector",
		numberOfKeysSuccessfullySend,
		keysToSendLength,
	)
}

func (pc *PeerCollectorHandler) SynchronizePeerCollector(ctx context.Context) error {
	pc.WrappedLogger.LogInfo("Start SynchronizePeerCollector")
	pc.ValidateLocalObjectsInObjectsInspectionList(ctx)
	pc.SendKeysToPeerCollector(ctx)
	return nil
}

func (pc *PeerCollectorHandler) SynchronizationLoop(ctx context.Context, pollTime time.Duration) error {
	syncLoopTticker := time.NewTicker(pollTime)
	defer syncLoopTticker.Stop()
	for {
		ctxSynchronize, cancelSynchronize := context.WithTimeout(
				ctx,
				time.Duration(pollTime.Seconds() - 5) * time.Second,
			)

		defer cancelSynchronize()
		err := pc.SynchronizePeerCollector(ctxSynchronize)
		if err != nil {
			pc.WrappedLogger.LogErrorf("Synchronizing PeerCollector failed. Continuing synchronization loop. Error (%s)", err)
		}

		select {
		case <- syncLoopTticker.C:
		case <-ctx.Done():
			pc.WrappedLogger.LogInfo("PeerConnector Synchronization Loop Done")
			return ctx.Err()
		}
	}
	return nil
}
