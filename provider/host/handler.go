package host

import (
	"context"

	"go.uber.org/zap"

	nodetypes "github.com/initia-labs/opinit-bots/node/types"
)

func (b *BaseHost) UpdateOracleConfigHandler(_ context.Context, args nodetypes.EventHandlerArgs) error {
	bridgeId, oracleEnabled, err := ParseMsgUpdateOracleConfig(args.EventAttributes)
	if err != nil {
		return err
	}
	if bridgeId != b.BridgeId() {
		// pass other bridge deposit event
		return nil
	}

	b.Logger().Info("update oracle config",
		zap.Bool("oracle_enabled", oracleEnabled),
	)

	b.nextOracleEnabled = oracleEnabled
	return nil
}
