package fdfs_client

import (
	"fmt"
	"net"
	"sync"
)

type Client struct {
	trackerPools	map[string]*ConnPool
	storagePools	map[string]*ConnPool
	storagePoolLock *sync.RWMutex
	config			*Config
}

func NewClientWithConfig(configName string) (*Client, error) {
	config, err := NewConfig(configName)
	if err != nil {
		return nil, err
    }
	client := &Client{
		config:					config,
		storagePoolLock:		&sync.RWMutex{},
	}
	client.trackerPools = make(map[string]*ConnPool)
	client.storagePools = make(map[string]*ConnPool)

	for _, addr := range config.trackerAddr {
		trackerPool, err := NewConnPool(addr, config.maxConns)
		if err != nil {
			return nil, err
		}
		client.trackerPools[addr] = trackerPool
    }

	return client, nil
}

func (this *Client) Destory() {
	if this == nil {
		return
    }
	for _, pool := range this.trackerPools {
		pool.Destory()
    }
	for _, pool := range this.storagePools {
		pool.Destory()
    }
}

func (this *Client) UploadByFilename(fileName string) (*FileId, error) {
	fileInfo, err := newFileInfo(fileName)
	defer fileInfo.Close()
	if err != nil {
		return nil, err
	}

	storageInfo, err := this.queryStorageInfoWithTracker(TRACKER_PROTO_CMD_SERVICE_QUERY_STORE_WITHOUT_GROUP_ONE,"","")
	if err != nil {
		return nil, err
	}

	task := &StorageUploadTask{}
	//req
	task.fileInfo = fileInfo
	task.storagePathIndex = storageInfo.storagePathIndex

	if err := this.doStorage(task, storageInfo);err != nil {
		return nil, err
    }
	return task.fileId,nil
}

func (this *Client) DownloadByFileId(fileId string,localFilename string) error {
	groupName, remoteFilename, err := SplitFileId(fileId)
	if err != nil {
		return err
	}
	storageInfo, err := this.queryStorageInfoWithTracker(TRACKER_PROTO_CMD_SERVICE_QUERY_FETCH_ONE,groupName,remoteFilename)
	if err != nil {
		return err
	}

	task := &StorageDownloadTask{}
	//req
	task.groupName = groupName
	task.remoteFilename = remoteFilename
	//task.offset = 0
	//task.downloadBytes = 0

	//res
	task.localFilename = localFilename

	return this.doStorage(task, storageInfo)
}

func (this *Client) DeleteByFileId(fileId string) error {
	groupName, remoteFilename, err := SplitFileId(fileId)
	if err != nil {
		return err
	}
	storageInfo, err := this.queryStorageInfoWithTracker(TRACKER_PROTO_CMD_SERVICE_QUERY_FETCH_ONE,groupName,remoteFilename)
	if err != nil {
		return err
	}

	task := &StorageDeleteTask{}
	//req
	task.groupName = groupName
	task.remoteFilename = remoteFilename

	return this.doStorage(task, storageInfo)
}

func (this *Client) doTracker(task task) error {
	trackerConn, err := this.getTrackerConn()
	defer trackerConn.Close()
	if err != nil {
		return err
	}
	if err := task.sendReq(trackerConn);err != nil{
		return err
    }
	if err := task.recvRes(trackerConn);err != nil{
		return err
    }
	
	return nil
}

func (this *Client) doStorage(task task, storageInfo *StorageInfo) error {
	storageConn, err := this.getStorageConn(storageInfo)
	defer storageConn.Close()
	if err != nil {
		return err
	}
	if err := task.sendReq(storageConn);err != nil{
		return err
    }
	if err := task.recvRes(storageConn);err != nil{
		return err
    }
	
	return nil
}

func (this *Client) queryStorageInfoWithTracker(cmd int8,groupName string,remoteFilename string) (*StorageInfo, error) {
	task := &TrackerTask{}
	task.cmd = cmd
	task.groupName = groupName
	task.remoteFilename = remoteFilename

	if err := this.doTracker(task);err != nil {
		return nil, err
    }
	return &StorageInfo{
		addr:             fmt.Sprintf("%s:%d", task.ipAddr, task.port),
		storagePathIndex: task.storePathIndex,
	}, nil
}

func (this *Client) getTrackerConn() (net.Conn, error) {
	var trackerConn net.Conn
	var err error
	var getOne bool
	for _, trackerPool := range this.trackerPools {
		trackerConn, err = trackerPool.get()
		if err == nil {
			getOne = true
			break
		}
	}
	if getOne {
		return trackerConn, nil
	}
	if err == nil {
		return nil, fmt.Errorf("no connPool can be use")
	}
	return nil, err
}

func (this *Client) getStorageConn(storageInfo *StorageInfo) (net.Conn, error) {
	this.storagePoolLock.Lock()
	storagePool, ok := this.storagePools[storageInfo.addr]
	if ok {
		this.storagePoolLock.Unlock()
		return storagePool.get()
	}
	storagePool, err := NewConnPool(storageInfo.addr,this.config.maxConns)
	if err != nil {
		this.storagePoolLock.Unlock()
		return nil, err
	}
	this.storagePools[storageInfo.addr] = storagePool
	this.storagePoolLock.Unlock()
	return storagePool.get()
}
