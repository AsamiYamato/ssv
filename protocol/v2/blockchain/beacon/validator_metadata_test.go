package beacon

import (
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/logging"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/utils/tasks"
)

func TestValidatorMetadata_Status(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: eth2apiv1.ValidatorStateActiveOngoing,
		}
		require.True(t, meta.Activated())
		require.False(t, meta.Exiting())
		require.False(t, meta.Slashed())
		require.False(t, meta.Pending())
	})

	t.Run("exited", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: eth2apiv1.ValidatorStateWithdrawalPossible,
		}
		require.True(t, meta.Exiting())
		require.True(t, meta.Activated())
		require.False(t, meta.Slashed())
		require.False(t, meta.Pending())
	})

	t.Run("exitedSlashed", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: eth2apiv1.ValidatorStateExitedSlashed,
		}
		require.True(t, meta.Slashed())
		require.True(t, meta.Exiting())
		require.True(t, meta.Activated())
		require.False(t, meta.Pending())
	})

	t.Run("pending", func(t *testing.T) {
		meta := &ValidatorMetadata{
			Status: eth2apiv1.ValidatorStatePendingQueued,
		}
		require.True(t, meta.Pending())
		require.False(t, meta.Slashed())
		require.False(t, meta.Exiting())
		require.False(t, meta.Activated())
	})
}

func TestUpdateValidatorsMetadata(t *testing.T) {
	logger := logging.TestLogger(t)

	var updateCount uint64
	pks := []string{
		"a17bb48a3f8f558e29d08ede97d6b7b73823d8dc2e0530fe8b747c93d7d6c2755957b7ffb94a7cec830456fd5492ba19",
		"a0cf5642ed5aa82178a5f79e00292c5b700b67fbf59630ce4f542c392495d9835a99c826aa2459a67bc80867245386c6",
		"8bafb7165f42e1179f83b7fcd8fe940e60ed5933fac176fdf75a60838c688eaa3b57717be637bde1d5cebdadb8e39865",
	}

	decodeds := make([][]byte, len(pks))
	blsPubKeys := make([]phase0.BLSPubKey, len(pks))
	for i, pk := range pks {
		blsPubKey := phase0.BLSPubKey{}
		decoded, _ := hex.DecodeString(pk)
		copy(blsPubKey[:], decoded)
		decodeds[i] = decoded
		blsPubKeys[i] = blsPubKey
	}

	// bc := beacon.NewMockBeacon(map[spectypes.OperatorID][]*Duty{}, data)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bc := NewMockBeacon(ctrl)
	bc.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
		validatorsData := map[phase0.BLSPubKey]*eth2apiv1.Validator{
			blsPubKeys[0]: {
				Index:     phase0.ValidatorIndex(210961),
				Status:    eth2apiv1.ValidatorStateWithdrawalPossible,
				Validator: &phase0.Validator{PublicKey: blsPubKeys[0]},
			},
			blsPubKeys[1]: {
				Index:     phase0.ValidatorIndex(213820),
				Status:    eth2apiv1.ValidatorStateActiveOngoing,
				Validator: &phase0.Validator{PublicKey: blsPubKeys[1]},
			},
		}

		results := map[phase0.ValidatorIndex]*eth2apiv1.Validator{}
		for _, pk := range validatorPubKeys {
			if data, ok := validatorsData[pk]; ok {
				results[data.Index] = data
			}
		}
		return results, nil
	})

	storageData := make(map[string]*ValidatorMetadata)
	storageMu := sync.Mutex{}

	// storage := NewMockValidatorMetadataStorage()
	storage := NewMockValidatorMetadataStorage(ctrl)
	storage.EXPECT().UpdateValidatorMetadata(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(logger *zap.Logger, pk string, metadata *ValidatorMetadata) error {
		storageMu.Lock()
		defer storageMu.Unlock()

		storageData[pk] = metadata

		return nil
	}).AnyTimes()

	onUpdated := func(logger *zap.Logger, pk string, meta *ValidatorMetadata) {
		joined := strings.Join(pks, ":")
		require.True(t, strings.Contains(joined, pk))
		require.True(t, meta.Index == phase0.ValidatorIndex(210961) || meta.Index == phase0.ValidatorIndex(213820))
		atomic.AddUint64(&updateCount, 1)
	}
	err := UpdateValidatorsMetadata(logger, [][]byte{blsPubKeys[0][:], blsPubKeys[1][:], blsPubKeys[2][:]}, storage, bc, onUpdated)
	require.Nil(t, err)
	require.Equal(t, uint64(2), updateCount)

	storageMu.Lock()
	storageSize := len(storageData)
	storageMu.Unlock()
	require.Equal(t, 2, storageSize)
}

func TestBatch(t *testing.T) {
	pks := []string{
		"a17bb48a3f8f558e29d08ede97d6b7b73823d8dc2e0530fe8b747c93d7d6c2755957b7ffb94a7cec830456fd5492ba19",
		"a0cf5642ed5aa82178a5f79e00292c5b700b67fbf59630ce4f542c392495d9835a99c826aa2459a67bc80867245386c6",
		"8bafb7165f42e1179f83b7fcd8fe940e60ed5933fac176fdf75a60838c688eaa3b57717be637bde1d5cebdadb8e39865",
		"aaafb7165f42e1179f83b7fcd8fe940e60ed5933fac176fdf75a60838c688eaa3b57717be637bde1d5cebdadb8e39865",
		"bbbfb7165f42e1179f83b7fcd8fe940e60ed5933fac176fdf75a60838c688eaa3b57717be637bde1d5cebdadb8e39865",
		"cccfb7165f42e1179f83b7fcd8fe940e60ed5933fac176fdf75a60838c688eaa3b57717be637bde1d5cebdadb8e39865",
	}

	decodeds := make([][]byte, 0)
	blsPubKeys := make([]phase0.BLSPubKey, len(pks))
	for i, pk := range pks {
		blsPubKey := phase0.BLSPubKey{}
		decoded, _ := hex.DecodeString(pk)
		copy(blsPubKey[:], decoded)
		decodeds = append(decodeds, decoded)
		blsPubKeys[i] = blsPubKey
	}

	t.Run("multiple batches", func(t *testing.T) {
		called := make([][]byte, 0)
		var wg sync.WaitGroup
		wg.Add(2)
		batch(decodeds, tasks.NewExecutionQueue(time.Millisecond), func(pks [][]byte) func() error {
			defer wg.Done()
			require.True(t, len(pks) > 0)
			called = append(called, pks...)
			return nil
		}, 4)
		wg.Wait()
		require.Equal(t, len(pks), len(called))
	})

	t.Run("single batch", func(t *testing.T) {
		called := make([][]byte, 0)
		var wg sync.WaitGroup
		wg.Add(1)
		batch(decodeds, tasks.NewExecutionQueue(time.Millisecond), func(pks [][]byte) func() error {
			defer wg.Done()
			require.Equal(t, len(decodeds), len(pks))
			called = append(called, pks...)
			return nil
		}, 25)
		wg.Wait()
		require.Equal(t, len(pks), len(called))
	})

	t.Run("no items", func(t *testing.T) {
		batch(make([][]byte, 0), tasks.NewExecutionQueue(time.Millisecond), func(pks [][]byte) func() error {
			t.Fail()
			return nil
		}, 4)
		time.Sleep(10 * time.Millisecond) // to be sure the function has finished
	})
}
