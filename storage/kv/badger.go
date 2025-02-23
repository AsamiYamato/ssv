package kv

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/bloxapp/ssv/logging/fields"

	"github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/storage/basedb"
)

const (
	// EntryNotFoundError is an error for a storage entry not found
	EntryNotFoundError = "EntryNotFoundError"
)

// BadgerDb struct
type BadgerDb struct {
	db *badger.DB

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// gcMutex is used to ensure that only one GC cycle is running at a time.
	gcMutex sync.Mutex
}

// New create new instance of Badger db
func New(logger *zap.Logger, options basedb.Options) (basedb.IDb, error) {
	// Open the Badger database located in the /tmp/badger directory.
	// It will be created if it doesn't exist.
	opt := badger.DefaultOptions(options.Path)

	if options.Type == "badger-memory" {
		opt.InMemory = true
		opt.Dir = ""
		opt.ValueDir = ""
	}

	// TODO: we should set the default logger here to log Error and higher levels
	opt.Logger = newLogger(zap.NewNop())
	if logger != nil && options.Reporting {
		opt.Logger = newLogger(logger)
	} else {
		opt.Logger = newLogger(zap.NewNop()) // TODO: we should allow only errors to be logged
	}

	opt.ValueLogFileSize = 1024 * 1024 * 100 // TODO:need to set the vlog proper (max) size
	db, err := badger.Open(opt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open badger")
	}

	// Set up context/cancel to control background goroutines.
	parentCtx := options.Ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(parentCtx)

	_db := BadgerDb{
		db:     db,
		ctx:    ctx,
		cancel: cancel,
	}

	// Start periodic reporting.
	if options.Reporting && options.Ctx != nil {
		_db.wg.Add(1)
		go _db.periodicallyReport(logger, 1*time.Minute)
	}

	// Start periodic garbage collection.
	if options.GCInterval > 0 {
		_db.wg.Add(1)
		go _db.periodicallyCollectGarbage(logger, options.GCInterval)
	}

	return &_db, nil
}

// Badger returns the underlying badger.DB
func (b *BadgerDb) Badger() *badger.DB {
	return b.db
}

// Set save value with key to storage
func (b *BadgerDb) Set(prefix []byte, key []byte, value []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerTxn{txn}.Set(prefix, key, value)
	})
}

// SetMany save many values with the given keys in a single badger transaction
func (b *BadgerDb) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	wb := b.db.NewWriteBatch()
	for i := 0; i < n; i++ {
		item, err := next(i)
		if err != nil {
			wb.Cancel()
			return err
		}
		if err := wb.Set(append(prefix, item.Key...), item.Value); err != nil {
			wb.Cancel()
			return err
		}
	}
	return wb.Flush()
}

// Get return value for specified key
func (b *BadgerDb) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	txn := b.db.NewTransaction(false)
	defer txn.Discard()
	return badgerTxn{txn}.Get(prefix, key)
}

// GetMany return values for the given keys
func (b *BadgerDb) GetMany(logger *zap.Logger, prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	if len(keys) == 0 {
		return nil
	}
	err := b.db.View(func(txn *badger.Txn) error {
		var value, cp []byte
		for _, k := range keys {
			item, err := txn.Get(append(prefix, k...))
			if err != nil {
				if isNotFoundError(err) { // in order to couple the not found errors together
					logger.Debug("item not found", zap.String("key", string(k)))
					continue
				}
				logger.Warn("failed to get item", zap.String("key", string(k)))
				return err
			}
			value, err = item.ValueCopy(value)
			if err != nil {
				logger.Warn("failed to copy item value", zap.String("key", string(k)))
				return err
			}
			cp = make([]byte, len(value))
			copy(cp, value)
			if err := iterator(basedb.Obj{
				Key:   k,
				Value: cp,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// Delete key in specific prefix
func (b *BadgerDb) Delete(prefix []byte, key []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return badgerTxn{txn}.Delete(prefix, key)
	})
}

// DeleteByPrefix all items with this prefix
func (b *BadgerDb) DeleteByPrefix(prefix []byte) (int, error) {
	count := 0
	err := b.db.Update(func(txn *badger.Txn) error {
		rawKeys := b.listRawKeys(prefix, txn)
		for _, k := range rawKeys {
			if err := txn.Delete(k); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

// GetAll returns all the items of a given collection
func (b *BadgerDb) GetAll(logger *zap.Logger, prefix []byte, handler func(int, basedb.Obj) error) error {
	// we got issues when reading more than 100 items with iterator (items get mixed up)
	// instead, the keys are first fetched using an iterator, and afterwards the values are fetched one by one
	// to avoid issues
	err := b.db.View(func(txn *badger.Txn) error {
		rawKeys := b.listRawKeys(prefix, txn)
		for i, k := range rawKeys {
			trimmedResKey := bytes.TrimPrefix(k, prefix)
			item, err := txn.Get(k)
			if err != nil {
				logger.Error("failed to get value", zap.Error(err),
					zap.String("trimmedResKey", string(trimmedResKey)))
				continue
			}
			val, err := item.ValueCopy(nil)
			if err != nil {
				logger.Error("failed to copy value", zap.Error(err))
				continue
			}
			if err := handler(i, basedb.Obj{
				Key:   trimmedResKey,
				Value: val,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// CountByCollection return the object count for all keys under specified prefix(bucket)
func (b *BadgerDb) CountByCollection(prefix []byte) (int64, error) {
	var res int64
	err := b.db.View(func(txn *badger.Txn) error {
		opt := badger.DefaultIteratorOptions
		opt.Prefix = prefix
		it := txn.NewIterator(opt)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			res++
		}
		return nil
	})
	return res, err
}

// RemoveAllByCollection cleans all items in a collection
func (b *BadgerDb) RemoveAllByCollection(prefix []byte) error {
	return b.db.DropPrefix(prefix)
}

// Close closes the database.
func (b *BadgerDb) Close(logger *zap.Logger) error {
	// Stop & wait for background goroutines.
	b.cancel()
	b.wg.Wait()

	// Close the database.
	err := b.db.Close()
	if err != nil {
		logger.Fatal("failed to close db", zap.Error(err))
	}
	return err
}

// report the db size and metrics
func (b *BadgerDb) report(logger *zap.Logger) func() {
	logger = logger.Named(logging.NameBadgerDBReporting)
	return func() {
		lsm, vlog := b.db.Size()
		blockCache := b.db.BlockCacheMetrics()
		indexCache := b.db.IndexCacheMetrics()

		logger.Debug("BadgerDBReport", zap.Int64("lsm", lsm), zap.Int64("vlog", vlog),
			fields.BlockCacheMetrics(blockCache),
			fields.IndexCacheMetrics(indexCache))
	}
}

func (b *BadgerDb) periodicallyReport(logger *zap.Logger, interval time.Duration) {
	defer b.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.report(logger)
		case <-b.ctx.Done():
			return
		}
	}
}

func (b *BadgerDb) listRawKeys(prefix []byte, txn *badger.Txn) [][]byte {
	var keys [][]byte

	opt := badger.DefaultIteratorOptions
	opt.Prefix = prefix
	it := txn.NewIterator(opt)
	defer it.Close()
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		keys = append(keys, item.KeyCopy(nil))
	}

	return keys
}

// Update is a gateway to badger db Update function
// creating and managing a read-write transaction
func (b *BadgerDb) Update(fn func(basedb.Txn) error) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return fn(&badgerTxn{txn: txn})
	})
}

func isNotFoundError(err error) bool {
	return err != nil && (err.Error() == "not found" || err.Error() == "Key not found")
}

type badgerTxn struct {
	txn *badger.Txn
}

func (t badgerTxn) Set(prefix []byte, key []byte, value []byte) error {
	return t.txn.Set(append(prefix, key...), value)
}

func (t badgerTxn) Get(prefix []byte, key []byte) (obj basedb.Obj, found bool, err error) {
	var resValue []byte
	item, err := t.txn.Get(append(prefix, key...))
	if err != nil {
		if isNotFoundError(err) { // in order to couple the not found errors together
			return basedb.Obj{}, false, nil
		}
		return basedb.Obj{}, true, err
	}
	resValue, err = item.ValueCopy(nil)
	if err != nil {
		return basedb.Obj{}, true, err
	}
	return basedb.Obj{
		Key:   key,
		Value: resValue,
	}, true, err
}

func (t badgerTxn) Delete(prefix []byte, key []byte) error {
	return t.txn.Delete(append(prefix, key...))
}
