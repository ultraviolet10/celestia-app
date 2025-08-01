package signal_test

import (
	"testing"

	"cosmossdk.io/core/header"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v6/test/util"
	"github.com/celestiaorg/celestia-app/v6/x/signal/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmversion "github.com/cometbft/cometbft/proto/tendermint/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestUpgradeIntegration uses the real application including the upgrade keeper (and staking keeper). It
// simulates an upgrade scenario with a single validator which signals for the version change, checks the quorum
// has been reached and then calls TryUpgrade, asserting that the upgrade module returns the new app version
func TestUpgradeIntegration(t *testing.T) {
	cp := app.DefaultConsensusParams()

	versionAfter := appconsts.Version
	versionBefore := versionAfter - 1
	cp.Version.App = versionBefore
	app, _ := testutil.SetupTestAppWithGenesisValSet(cp)
	ctx := sdk.NewContext(app.CommitMultiStore(), tmproto.Header{
		Version: tmversion.Consensus{
			App: versionBefore,
		},
		ChainID: appconsts.TestChainID,
	}, false, log.NewNopLogger()).WithHeaderInfo(header.Info{ChainID: appconsts.TestChainID})

	res, err := app.SignalKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: versionAfter,
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, res.VotingPower)

	validators, err := app.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	valAddr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(t, err)

	_, err = app.SignalKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: valAddr.String(),
		Version:          versionAfter,
	})
	require.NoError(t, err)

	res, err = app.SignalKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: versionAfter,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, res.VotingPower)
	require.EqualValues(t, 1, res.ThresholdPower)
	require.EqualValues(t, 1, res.TotalVotingPower)

	_, err = app.SignalKeeper.TryUpgrade(ctx, &types.MsgTryUpgrade{
		Signer: valAddr.String(),
	})
	require.NoError(t, err)

	// Verify that if a user queries the version tally, it still works after a
	// successful try upgrade.
	res, err = app.SignalKeeper.VersionTally(ctx, &types.QueryVersionTallyRequest{
		Version: versionAfter,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, res.VotingPower)
	require.EqualValues(t, 1, res.ThresholdPower)
	require.EqualValues(t, 1, res.TotalVotingPower)

	// Verify that if a subsequent call to TryUpgrade is made, it returns an
	// error because an upgrade is already pending.
	_, err = app.SignalKeeper.TryUpgrade(ctx, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUpgradePending)

	// Verify that if a validator tries to change their signal version, it
	// returns an error because an upgrade is pending.
	_, err = app.SignalKeeper.SignalVersion(ctx, &types.MsgSignalVersion{
		ValidatorAddress: valAddr.String(),
		Version:          versionAfter + 1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUpgradePending)

	shouldUpgrade, upgrade := app.SignalKeeper.ShouldUpgrade(ctx)
	require.False(t, shouldUpgrade)
	require.EqualValues(t, 0, upgrade.AppVersion)

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + appconsts.GetUpgradeHeightDelay(appconsts.TestChainID))

	shouldUpgrade, upgrade = app.SignalKeeper.ShouldUpgrade(ctx)
	require.True(t, shouldUpgrade)
	require.EqualValues(t, versionAfter, upgrade.AppVersion)
}
