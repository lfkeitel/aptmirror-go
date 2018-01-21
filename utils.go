package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var httpClient = &http.Client{Transport: &http.Transport{
	MaxIdleConns:    10,
	IdleConnTimeout: 30 * time.Second,
}}

func checkAndMakeDirPath(path string) error {
	if fileExists(path) {
		return nil
	}
	return os.MkdirAll(path, 0755)
}

func checkAndMakeFilePath(path string) error {
	return checkAndMakeDirPath(filepath.Dir(path))
}

func fileExists(file string) bool {
	_, err := os.Stat(file)
	return !os.IsNotExist(err)
}

func inSliceBytes(stack [][]byte, goal []byte) bool {
	for _, slice := range stack {
		if bytes.Equal(slice, goal) {
			return true
		}
	}
	return false
}

func verifySHA256File(file, hash string) bool {
	f, err := os.Open(file)
	if err != nil {
		log.Println(err)
		return false
	}
	defer f.Close()

	return verifySHA256Reader(f, hash)
}

func verifySHA256Reader(reader io.Reader, hash string) bool {
	h := sha256.New()
	if _, err := io.Copy(h, reader); err != nil {
		log.Println(err)
		return false
	}

	return fmt.Sprintf("%x", h.Sum(nil)) == hash
}

func formatFileSize(bytes int64) string {
	const (
		kib float64 = 1024
		mib float64 = 1024 * 1024
		gib float64 = 1024 * 1024 * 1024
	)

	bytesIn := float64(bytes)
	bytesOut := bytesIn
	unit := "bytes"

	if bytesIn >= kib {
		bytesOut = bytesIn / kib
		unit = "KiB"

		if bytesIn >= mib {
			bytesOut = bytesIn / mib
			unit = "MiB"

			if bytesIn >= gib {
				bytesOut = bytesIn / gib
				unit = "GiB"
			}
		}
	}

	return fmt.Sprintf("%.2f %s", bytesOut, unit)
}

func logDebug(msg string) {
	if debugMode {
		log.Println(msg)
	}
}

func logDebugf(msg string, args ...interface{}) {
	if debugMode {
		log.Printf(msg, args...)
	}
}
