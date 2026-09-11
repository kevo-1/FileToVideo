package services

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	OWNER_RW_OTHERS_R   = 0644 //Read/write for owner; read-only for group and others.
	OWNER_RW            = 0600 //Read/write for owner only; no access for anyone else
	OWNER_RWE_OTHERS_RE = 0755 //Read/write/execute for owner; read/execute for group and others.
	EVERY_RWE           = 0777 //Read, write, and execute permissions for everyone.
)

const UploadDir = "uploads"

// Handle creating temporary files for processing later on
func CreateTempFile(fileName, reqId string) (*os.File, error) {
	if err := os.MkdirAll(UploadDir, OWNER_RWE_OTHERS_RE); err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	// avoid path traversal by taking the base of the filename
	fileName = filepath.Base(fileName)

	dst, err := os.Create(filepath.Join(UploadDir, fmt.Sprintf("%s-%s", reqId, fileName)))

	if err != nil {
		return nil, err
	}

	return dst, nil
}

func DeleteTempFile(path string) error {
	err := os.Remove(path)
	if err != nil {
		return err
	}
	return nil
}
