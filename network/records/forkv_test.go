package records

import (
	crand "crypto/rand"
	"testing"

	"github.com/bloxapp/ssv/network/commons"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"
)

func Test_ForkVersionEntry(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	require.NoError(t, err)
	sk, err := commons.ConvertFromInterfacePrivKey(priv)
	require.NoError(t, err)
	ip, err := commons.IPAddr()
	require.NoError(t, err)
	node, err := CreateLocalNode(sk, "", ip, commons.DefaultUDP, commons.DefaultTCP)
	require.NoError(t, err)

	require.NoError(t, SetForkVersionEntry(node, forksprotocol.GenesisForkVersion.String()))
	t.Log("ENR with fork version:", node.Node().String())

	fv, err := GetForkVersionEntry(node.Node().Record())
	require.NoError(t, err)
	require.Equal(t, forksprotocol.ForkVersion(forksprotocol.GenesisForkVersion.String()), fv)
}
