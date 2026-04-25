package storeutil

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

func EnsureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o750)
}

func OpenDB(path string, timeout time.Duration) (*bolt.DB, error) {
	return bolt.Open(path, 0o600, &bolt.Options{Timeout: timeout})
}

func ReadVersion(metaBucket *bolt.Bucket, key []byte, defaultVersion int) (int, error) {
	if metaBucket == nil {
		return defaultVersion, nil
	}
	raw := strings.TrimSpace(string(metaBucket.Get(key)))
	if raw == "" {
		return defaultVersion, nil
	}
	version, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if version <= 0 {
		return defaultVersion, nil
	}
	return version, nil
}

func WriteVersion(metaBucket *bolt.Bucket, key []byte, version, defaultVersion int) error {
	if version <= 0 {
		version = defaultVersion
	}
	return metaBucket.Put(key, []byte(strconv.Itoa(version)))
}
