package txsim

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util/blobfactory"
	blob "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/grpc"
)

var _ Sequence = &BlobSequence{}

// As napkin math, this would cover the cost of 8267 4KB blobs
const fundsForGas int = 1e9 // 1000 TIA

// BlobSequence defines a pattern whereby a single user repeatedly sends a pay for blob
// message roughly every height. The PFB may consist of several blobs
type BlobSequence struct {
	namespaces    []share.Namespace
	sizes         Range
	blobsPerPFB   Range
	shareVersions []uint8

	account     types.AccAddress
	useFeegrant bool
	gasPrice    float64
}

func NewBlobSequence(sizes, blobsPerPFB Range) *BlobSequence {
	return &BlobSequence{
		sizes:         sizes,
		blobsPerPFB:   blobsPerPFB,
		shareVersions: []uint8{share.ShareVersionZero, share.ShareVersionOne},
	}
}

// WithNamespaces provides the option of fixing a set of predefined namespaces for
// all blobs.
func (s *BlobSequence) WithNamespaces(namespaces []share.Namespace) *BlobSequence {
	s.namespaces = namespaces
	return s
}

// WithShareVersion provides the option of fixing a predefined share version for
// all blobs else it will randomly select a share version for each blob.
func (s *BlobSequence) WithShareVersion(version uint8) *BlobSequence {
	if version != share.ShareVersionZero && version != share.ShareVersionOne {
		panic(fmt.Sprintf("invalid share version %d", version))
	}
	s.shareVersions = []uint8{version}
	return s
}

func (s *BlobSequence) WithGasPrice(gasPrice float64) *BlobSequence {
	s.gasPrice = gasPrice
	return s
}

func (s *BlobSequence) Clone(n int) []Sequence {
	sequenceGroup := make([]Sequence, n)
	for i := 0; i < n; i++ {
		sequenceGroup[i] = &BlobSequence{
			namespaces:    s.namespaces,
			sizes:         s.sizes,
			blobsPerPFB:   s.blobsPerPFB,
			shareVersions: s.shareVersions,
			gasPrice:      s.gasPrice,
		}
	}
	return sequenceGroup
}

func (s *BlobSequence) Init(_ context.Context, _ grpc.ClientConn, allocateAccounts AccountAllocator, _ *rand.Rand, useFeegrant bool) {
	s.useFeegrant = useFeegrant
	funds := fundsForGas
	if useFeegrant {
		funds = 1
	}
	s.account = allocateAccounts(1, funds)[0]
}

func (s *BlobSequence) Next(_ context.Context, _ grpc.ClientConn, rand *rand.Rand) (Operation, error) {
	numBlobs := s.blobsPerPFB.Rand(rand)
	sizes := make([]int, numBlobs)
	namespaces := make([]share.Namespace, numBlobs)
	for i := range sizes {
		if len(s.namespaces) > 0 {
			randIdx := rand.Intn(len(s.namespaces))
			namespaces[i] = s.namespaces[randIdx]
		} else {
			// generate a random namespace for the blob
			namespace := make([]byte, share.NamespaceVersionZeroIDSize)
			_, err := rand.Read(namespace)
			if err != nil {
				return Operation{}, fmt.Errorf("generating random namespace: %w", err)
			}
			namespaces[i] = share.MustNewV0Namespace(namespace)
		}
		sizes[i] = s.sizes.Rand(rand)
	}
	// generate the blobs
	var blobs []*share.Blob
	shareVersion := s.shareVersions[rand.Intn(len(s.shareVersions))]
	switch shareVersion {
	case share.ShareVersionZero:
		blobs = blobfactory.RandV0BlobsWithNamespace(namespaces, sizes)
	case share.ShareVersionOne:
		blobs = blobfactory.RandV1BlobsWithNamespace(namespaces, sizes, s.account)
	default:
		return Operation{}, fmt.Errorf("invalid share version: %d", shareVersion)
	}
	// derive the pay for blob message
	msg, err := blob.NewMsgPayForBlobs(s.account.String(), appconsts.Version, blobs...)
	if err != nil {
		return Operation{}, err
	}

	op := Operation{
		Msgs:     []types.Msg{msg},
		Blobs:    blobs,
		GasLimit: estimateGas(sizes, s.useFeegrant),
	}

	if s.gasPrice != 0 {
		op.GasPrice = s.gasPrice
	}

	return op, nil
}

type Range struct {
	Min int
	Max int
}

func NewRange(rangeMin, rangeMax int) Range {
	return Range{Min: rangeMin, Max: rangeMax}
}

// Rand returns a random number between min (inclusive) and max (exclusive).
func (r Range) Rand(rand *rand.Rand) int {
	if r.Max <= r.Min {
		return r.Min
	}
	return rand.Intn(r.Max-r.Min) + r.Min
}

// estimateGas estimates the gas required to pay for a set of blobs in a PFB.
func estimateGas(blobSizes []int, useFeegrant bool) uint64 {
	size := make([]uint32, len(blobSizes))
	for i, s := range blobSizes {
		size[i] = uint32(s)
	}

	// account for the extra gas required to pay for the fee granter
	extra := uint64(0)
	if useFeegrant {
		extra = 12000
	}

	return blob.DefaultEstimateGas(size) + extra
}
