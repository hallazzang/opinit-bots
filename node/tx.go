package node

import (
	"context"
	"fmt"
	"math"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	nodetypes "github.com/initia-labs/opinit-bots-go/node/types"
)

func (n *Node) BroadcastMsgs(processedMsgs nodetypes.ProcessedMsgs) error {
	ctx := context.Background()

	sequence := n.txf.Sequence()
	txBytes, err := n.BuildMessages(ctx, processedMsgs.Msgs)
	if err != nil {
		return err
	}

	res, err := n.BroadcastTxSync(ctx, txBytes)
	if err != nil {
		fmt.Println(res, err)
		// TODO: handle error, may repeat sending tx
		return fmt.Errorf("broadcast txs: %w", err)
	}

	n.txf = n.txf.WithSequence(n.txf.Sequence() + 1)
	pendingTx := nodetypes.PendingTxInfo{
		ProcessedHeight: n.GetHeight(),
		Sequence:        sequence,
		Tx:              txBytes,
		Save:            processedMsgs.Save,
	}
	err = n.savePendingTx(sequence, pendingTx)
	if err != nil {
		return err
	}
	n.appendLocalPendingTx(pendingTx)
	return nil
}

func (n Node) BuildMessages(
	ctx context.Context,
	msgs []sdk.Msg,
) (
	txBytes []byte,
	err error,
) {
	txf := n.txf

	_, adjusted, err := n.CalculateGas(ctx, txf, msgs...)
	if err != nil {
		return nil, err
	}

	// not use simulation for now to handle account's sequence locally
	// TODO: use simulation
	// n.txf = n.txf.WithGas(uint64(1_000_000 + nodetypes.PER_MSG_GAS_LIMIT*len(msgs)))
	txf = txf.WithGas(adjusted)
	txb, err := txf.BuildUnsignedTx(msgs...)
	if err != nil {
		return nil, err
	}

	if err = tx.Sign(ctx, txf, nodetypes.KEY_NAME, txb, false); err != nil {
		return nil, err
	}

	tx := txb.GetTx()
	txBytes, err = n.EncodeTx(tx)
	if err != nil {
		return nil, err
	}
	return txBytes, nil
}

// CalculateGas simulates a tx to generate the appropriate gas settings before broadcasting a tx.
func (n Node) CalculateGas(ctx context.Context, txf tx.Factory, msgs ...sdk.Msg) (txtypes.SimulateResponse, uint64, error) {
	keyInfo, err := n.keyBase.Key(nodetypes.KEY_NAME)
	if err != nil {
		return txtypes.SimulateResponse{}, 0, err
	}

	txBytes, err := BuildSimTx(keyInfo, txf, msgs...)
	if err != nil {
		return txtypes.SimulateResponse{}, 0, err
	}

	simQuery := abci.RequestQuery{
		Path: "/cosmos.tx.v1beta1.Service/Simulate",
		Data: txBytes,
	}

	res, err := n.QueryABCI(ctx, simQuery)
	if err != nil {
		return txtypes.SimulateResponse{}, 0, err
	}

	var simRes txtypes.SimulateResponse
	if err := simRes.Unmarshal(res.Value); err != nil {
		return txtypes.SimulateResponse{}, 0, err
	}

	gas, err := n.AdjustEstimatedGas(simRes.GasInfo.GasUsed)
	return simRes, gas, err
}

// AdjustEstimatedGas adjusts the estimated gas usage by multiplying it by the gas adjustment factor
// and return estimated gas is higher than max gas error. If the gas usage is zero, the adjusted gas
// is also zero.
func (n Node) AdjustEstimatedGas(gasUsed uint64) (uint64, error) {
	if gasUsed == 0 {
		return gasUsed, nil
	}

	gas := nodetypes.GAS_ADJUSTMENT * float64(gasUsed)
	if math.IsInf(gas, 1) {
		return 0, fmt.Errorf("infinite gas used")
	}
	return uint64(gas), nil
}

// BuildSimTx creates an unsigned tx with an empty single signature and returns
// the encoded transaction or an error if the unsigned transaction cannot be built.
func BuildSimTx(info *keyring.Record, txf tx.Factory, msgs ...sdk.Msg) ([]byte, error) {
	txb, err := txf.BuildUnsignedTx(msgs...)
	if err != nil {
		return nil, err
	}

	var pk cryptotypes.PubKey = &secp256k1.PubKey{} // use default public key type

	pk, err = info.GetPubKey()
	if err != nil {
		return nil, err
	}

	// Create an empty signature literal as the ante handler will populate with a
	// sentinel pubkey.
	sig := signing.SignatureV2{
		PubKey: pk,
		Data: &signing.SingleSignatureData{
			SignMode: txf.SignMode(),
		},
		Sequence: txf.Sequence(),
	}
	if err := txb.SetSignatures(sig); err != nil {
		return nil, err
	}

	protoProvider, ok := txb.(protoTxProvider)
	if !ok {
		return nil, fmt.Errorf("cannot simulate amino tx")
	}

	simReq := txtypes.SimulateRequest{Tx: protoProvider.GetProtoTx()}
	return simReq.Marshal()
}

// protoTxProvider is a type which can provide a proto transaction. It is a
// workaround to get access to the wrapper TxBuilder's method GetProtoTx().
type protoTxProvider interface {
	GetProtoTx() *txtypes.Tx
}

func (n *Node) appendLocalPendingTx(tx nodetypes.PendingTxInfo) {
	n.pendingTxMu.Lock()
	defer n.pendingTxMu.Unlock()

	n.pendingTxs = append(n.pendingTxs, tx)
}

func (n *Node) getLocalPendingTx() nodetypes.PendingTxInfo {
	n.pendingTxMu.Lock()
	defer n.pendingTxMu.Unlock()

	return n.pendingTxs[0]
}

func (n *Node) localPendingTxLength() int {
	n.pendingTxMu.Lock()
	defer n.pendingTxMu.Unlock()

	return len(n.pendingTxs)
}

func (n *Node) deleteLocalPendingTx() {
	n.pendingTxMu.Lock()
	defer n.pendingTxMu.Unlock()

	n.pendingTxs = n.pendingTxs[1:]
}

func (n *Node) EncodeTx(tx authsigning.Tx) ([]byte, error) {
	txBytes, err := n.txConfig.TxEncoder()(tx)
	if err != nil {
		return nil, err
	}
	return txBytes, nil
}

func (n *Node) DecodeTx(txBytes []byte) (authsigning.Tx, error) {
	tx, err := n.txConfig.TxDecoder()(txBytes)
	if err != nil {
		return nil, err
	}
	return tx.(authsigning.Tx), nil
}
