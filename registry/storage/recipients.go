package storage

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/storage/basedb"
)

var (
	recipientsPrefix = []byte("recipients")
)

// RecipientData the public data of a recipient
type RecipientData struct {
	Owner        common.Address             `json:"ownerAddress"`
	FeeRecipient bellatrix.ExecutionAddress `json:"feeRecipientAddress"`
}

// Recipients is the interface for managing recipients data
type Recipients interface {
	GetRecipientData(owner common.Address) (*RecipientData, bool, error)
	GetRecipientDataMany(logger *zap.Logger, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error)
	SaveRecipientData(recipientData *RecipientData) (*RecipientData, error)
	DeleteRecipientData(owner common.Address) error
	GetRecipientsPrefix() []byte
}

type recipientsStorage struct {
	db     basedb.IDb
	lock   sync.RWMutex
	prefix []byte
}

// NewRecipientsStorage creates a new instance of Storage
func NewRecipientsStorage(db basedb.IDb, prefix []byte) Recipients {
	return &recipientsStorage{
		db:     db,
		prefix: prefix,
	}
}

// GetRecipientsPrefix returns the prefix
func (s *recipientsStorage) GetRecipientsPrefix() []byte {
	return recipientsPrefix
}

// GetRecipientData returns data of the given recipient by owner address, if not found returns owner address as a default fee recipient
func (s *recipientsStorage) GetRecipientData(owner common.Address) (*RecipientData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getRecipientData(owner)
}

func (s *recipientsStorage) getRecipientData(owner common.Address) (*RecipientData, bool, error) {
	obj, found, err := s.db.Get(s.prefix, buildRecipientKey(owner))
	if err != nil {
		return nil, found, err
	}
	if !found {
		return nil, found, nil
	}
	var recipientData RecipientData
	err = json.Unmarshal(obj.Value, &recipientData)
	return &recipientData, found, err
}

func (s *recipientsStorage) GetRecipientDataMany(logger *zap.Logger, owners []common.Address) (map[common.Address]bellatrix.ExecutionAddress, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var keys [][]byte
	for _, owner := range owners {
		keys = append(keys, buildRecipientKey(owner))
	}
	results := make(map[common.Address]bellatrix.ExecutionAddress)
	err := s.db.GetMany(logger, s.prefix, keys, func(obj basedb.Obj) error {
		var recipient RecipientData
		err := json.Unmarshal(obj.Value, &recipient)
		if err != nil {
			return errors.Wrap(err, "could not unmarshal recipient data")
		}
		results[recipient.Owner] = recipient.FeeRecipient
		return nil
	})
	if err != nil {
		return nil, err
	}

	return results, nil
}

// SaveRecipientData saves recipient data and return it.
// if the recipient already exists and the fee didn't change return nil
func (s *recipientsStorage) SaveRecipientData(recipientData *RecipientData) (*RecipientData, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	r, found, err := s.getRecipientData(recipientData.Owner)
	if err != nil {
		return nil, errors.Wrap(err, "could not get recipient data")
	}
	// same fee recipient
	if found && r.FeeRecipient == recipientData.FeeRecipient {
		return nil, nil
	}

	raw, err := json.Marshal(recipientData)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal recipient data")
	}
	return recipientData, s.db.Set(s.prefix, buildRecipientKey(recipientData.Owner), raw)
}

func (s *recipientsStorage) DeleteRecipientData(owner common.Address) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.db.Delete(s.prefix, buildRecipientKey(owner))
}

// buildRecipientKey builds recipient key using recipientsPrefix & owner address, e.g. "recipients/0x00..01"
func buildRecipientKey(owner common.Address) []byte {
	return bytes.Join([][]byte{recipientsPrefix, owner.Bytes()}, []byte("/"))
}
