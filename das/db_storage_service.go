// Copyright 2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package das

import (
	"context"
	"github.com/dgraph-io/badger"
	"time"
)

type DBStorageService struct {
	db                  *badger.DB
	discardAfterTimeout bool
	dirPath             string
}

func NewDBStorageService(ctx context.Context, dirPath string, discardAfterTimeout bool) (StorageService, error) {
	db, err := badger.Open(badger.DefaultOptions(dirPath))
	if err != nil {
		return nil, err
	}

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		defer func() { _ = db.Close() }()
		for {
			select {
			case <-ticker.C:
				for db.RunValueLogGC(0.7) == nil {
					select {
					case <-ctx.Done():
						return
					default:
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return &DBStorageService{db, discardAfterTimeout, dirPath}, nil
}

func (dbs *DBStorageService) Read(ctx context.Context, key []byte) ([]byte, error) {
	var ret []byte
	err := dbs.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			ret = append([]byte{}, val...)
			return nil
		})
	})
	return ret, err
}

func (dbs *DBStorageService) Write(ctx context.Context, key []byte, value []byte, timeout uint64) error {
	return dbs.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value) // TODO: honor discardAfterTimeout
	})
}

func (dbs *DBStorageService) Sync(ctx context.Context) error {
	return dbs.db.Sync()
}

func (dbs *DBStorageService) String() string {
	return "BadgerDB(" + dbs.dirPath + ")"
}