package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	OwnerRWOthersR   = 0644 //Read/write for owner; read-only for group and others.
	OwnerRW          = 0600 //Read/write for owner only; no access for anyone else
	OwnerRWEOthersRE = 0755 //Read/write/execute for owner; read/execute for group and others.
	EveryoneRWE      = 0777 //Read, write, and execute permissions for everyone.
)

type TempFile struct {
	ReqId    string
	FileName string
	Handle   *os.File
	path     string
}

func (tf *TempFile) Path() string { return tf.path }

func (tf *TempFile) Close() error {
	if tf.Handle == nil {
		return nil
	}
	err := tf.Handle.Close()
	tf.Handle = nil
	return err
}

func (tf *TempFile) Write(p []byte) (int, error) {
	if tf.Handle == nil {
		return 0, fmt.Errorf("temp file not ready")
	}
	return tf.Handle.Write(p)
}

const UploadDir = "uploads"

type TempFileRepo struct {
	mu      sync.Mutex
	Files   map[string]*TempFile
	BaseDir string
}

func NewTempFileRepo() (*TempFileRepo, error) {
	if err := os.MkdirAll(UploadDir, OwnerRWEOthersRE); err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	return &TempFileRepo{
		Files:   make(map[string]*TempFile),
		BaseDir: UploadDir,
	}, nil
}

func (tfr *TempFileRepo) CreateTempFile(fileName, reqId string) (*TempFile, error) {
	fileName = filepath.Base(fileName)
	path := filepath.Join(tfr.BaseDir, fmt.Sprintf("%s-%s", reqId, fileName))

	tfr.mu.Lock()
	if _, ok := tfr.Files[reqId]; ok {
		tfr.mu.Unlock()
		return nil, fmt.Errorf("creating temp file: temporary file with this id already exists")
	}

	tfr.Files[reqId] = &TempFile{ReqId: reqId, FileName: fileName, path: path}
	tfr.mu.Unlock()

	dst, err := os.Create(path)
	if err != nil {
		tfr.mu.Lock()
		delete(tfr.Files, reqId)
		tfr.mu.Unlock()
		return nil, fmt.Errorf("creating temp file: %w", err)
	}

	tfr.mu.Lock()
	entry, ok := tfr.Files[reqId]
	if !ok {
		tfr.mu.Unlock()
		_ = dst.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("creating temp file: reservation for id = %s was removed concurrently", reqId)
	}
	entry.Handle = dst
	tfr.mu.Unlock()

	return entry, nil
}

func (tfr *TempFileRepo) GetTempFileById(reqId string) (*TempFile, error) {
	if reqId == "" {
		return nil, fmt.Errorf("fetching temp file: request id is required")
	}

	tfr.mu.Lock()
	tmpFile, ok := tfr.Files[reqId]
	tfr.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("fetching temp file: couldn't find file by id = %s", reqId)
	}

	return tmpFile, nil
}

func (tfr *TempFileRepo) DeleteTempFile(reqId string) error {
	tfr.mu.Lock()
	tmpFile, ok := tfr.Files[reqId]
	if ok {
		delete(tfr.Files, reqId)
	}
	tfr.mu.Unlock()

	if !ok {
		return fmt.Errorf("deleting temp file: couldn't find file by id = %s", reqId)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("deleting temp file: closing handle: %w", err)
	}

	if err := os.Remove(tmpFile.path); err != nil {
		return fmt.Errorf("deleting temp file: %w", err)
	}

	return nil
}

func (tfr *TempFileRepo) Close() error {
	tfr.mu.Lock()
	for _, tf := range tfr.Files {
		_ = tf.Close()
	}
	clear(tfr.Files)
	tfr.mu.Unlock()

	if err := os.RemoveAll(tfr.BaseDir); err != nil {
		return fmt.Errorf("removing uploads dir: %w", err)
	}

	return nil
}
