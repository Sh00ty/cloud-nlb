package sharder

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/cespare/xxhash"
	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/models"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
)

type ShardingNodeID int

type Vshard uint

const nodeIDPrefix = "hc-worker-"

func NodeIDToChID(nodeID models.NodeID) ShardingNodeID {
	orderStr := nodeID[len(nodeIDPrefix):]
	order, err := strconv.Atoi(orderStr.String())
	if err != nil {
		panic(err)
	}
	return ShardingNodeID(order)
}

func (n ShardingNodeID) ToNodeID() models.NodeID {
	return models.NodeID(fmt.Sprintf("%s%d", nodeIDPrefix, n))
}

type ConsistentHashing interface {
	MarkUnhealthy(ShardingNodeID) error
	MarkHealthy(ShardingNodeID) error
	GetWithOffset(key int64, backupNum uint) (ShardingNodeID, error)
}

type ConsistentSharder struct {
	MyNodeID          models.NodeID
	replicationFactor uint16

	// vshard -> nodeid
	myVShards      map[Vshard]struct{}
	checksByVShard map[Vshard]map[string]struct{}

	vShardCount uint64

	vShardsIpSharder   ConsistentHashing
	nodeVShardsSharder ConsistentHashing
}

func NewConsistentSharder(
	nodesCh ConsistentHashing,
	vshardCh ConsistentHashing,
	vshardCount uint64,
	replicationFactor uint16,
	myNode models.NodeID,
) (*ConsistentSharder, error) {
	const bigSearchLimmit = 1024 * 1024
	var (
		checksByVShard = make(map[Vshard]map[string]struct{}, vshardCount)
		myVShards      = make(map[Vshard]struct{})
	)
	for i := range vshardCount {
		vshard := Vshard(i)
		checksByVShard[vshard] = make(map[string]struct{})
		myVShards[vshard] = struct{}{}

		vshardCh.MarkHealthy(ShardingNodeID(vshard))
	}
	nodesCh.MarkHealthy(NodeIDToChID(myNode))
	return &ConsistentSharder{
		MyNodeID:           myNode,
		myVShards:          myVShards,
		replicationFactor:  replicationFactor,
		vShardCount:        vshardCount,
		vShardsIpSharder:   vshardCh,
		nodeVShardsSharder: nodesCh,
		checksByVShard:     checksByVShard,
	}, nil
}

func (s *ConsistentSharder) GetTargetVshard(targetKey string) Vshard {
	targetHash := xxhash.Sum64([]byte(targetKey))
	vs, err := s.vShardsIpSharder.GetWithOffset(int64(targetHash), 0)
	if err != nil {
		panic(err)
	}
	return Vshard(vs)
}

func (s *ConsistentSharder) getNodeIDsByVshard(vs Vshard) []models.NodeID {
	nodesForVshard := make([]models.NodeID, 0, s.replicationFactor)
	for i := range uint(s.replicationFactor) {
		nodeID, err := s.nodeVShardsSharder.GetWithOffset(int64(vs), i)
		if err != nil {
			panic(err)
		}
		nodesForVshard = append(nodesForVshard, nodeID.ToNodeID())
	}
	return nodesForVshard
}

func (s *ConsistentSharder) LinkTarget(target healthcheck.TargetAddr) bool {
	var (
		targetKey = target.String()
		vshard    = s.GetTargetVshard(targetKey)
	)

	if _, exists := s.checksByVShard[vshard][targetKey]; exists {
		return false
	}
	s.checksByVShard[vshard][targetKey] = struct{}{}
	return true
}

func (s *ConsistentSharder) RemoveTargetLink(target healthcheck.TargetAddr) bool {
	var (
		targetKey = target.String()
		vshard    = s.GetTargetVshard(targetKey)
	)

	if _, exists := s.checksByVShard[vshard][targetKey]; !exists {
		return false
	}
	delete(s.checksByVShard[vshard], targetKey)
	return true
}

// Добавилась новая нода => она забирает шарды =>
// с нашей ноды шарды могли только уйти
func (s *ConsistentSharder) AddNewMember(ctx context.Context, nodeID models.NodeID) ([]healthcheck.TargetAddr, error) {
	if nodeID == models.NodeID(s.MyNodeID) {
		return nil, nil
	}
	s.nodeVShardsSharder.MarkHealthy(NodeIDToChID(nodeID))

	dropTargets := make([]healthcheck.TargetAddr, 0, 128)
	for vshard := range Vshard(s.vShardCount) {

		nodeIDsforVShard := s.getNodeIDsByVshard(vshard)

		if !slices.Contains(nodeIDsforVShard, nodeID) {
			continue
		}
		_, exists := s.myVShards[vshard]
		if !exists || slices.Contains(nodeIDsforVShard, s.MyNodeID) {
			// шард не был нашим или мы до сих пор его содержим
			continue
		}
		delete(s.myVShards, vshard)
		for targetStr := range s.checksByVShard[vshard] {
			targetAddr, err := healthcheck.TargetAddrFromString(targetStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse target addr: %s: %w", targetStr, err)
			}
			dropTargets = append(dropTargets, targetAddr)
		}
		s.checksByVShard[vshard] = make(map[string]struct{})
	}
	return dropTargets, nil
}

func (s *ConsistentSharder) RemoveMember(ctx context.Context, nodeID models.NodeID) ([]uint, error) {
	s.nodeVShardsSharder.MarkUnhealthy(NodeIDToChID(nodeID))

	shardsToFetch := make([]uint, 0, s.vShardCount)
	for vshard := range Vshard(s.vShardCount) {
		nodeIDsforVShard := s.getNodeIDsByVshard(vshard)
		if !slices.Contains(nodeIDsforVShard, s.MyNodeID) {
			continue
		}
		_, exists := s.myVShards[vshard]
		if exists {
			continue
		}
		shardsToFetch = append(shardsToFetch, uint(vshard))
		s.myVShards[vshard] = struct{}{}
	}
	return shardsToFetch, nil
}

func (s *ConsistentSharder) NeedHandle(target healthcheck.TargetAddr) bool {
	vshard := s.GetTargetVshard(target.String())
	nodeIDs := s.getNodeIDsByVshard(vshard)

	log.Debug().Msgf("target %+v assigned to vshard=%d and nodes=%v", target.String(), vshard, nodeIDs)
	return slices.Contains(nodeIDs, s.MyNodeID)
}

func (s *ConsistentSharder) GetVshardCount() uint64 {
	return s.vShardCount
}

func (s *ConsistentSharder) GetMyVshards() []uint {
	myVShards := make([]uint, 0, len(s.myVShards))
	for myVShard := range s.myVShards {
		myVShards = append(myVShards, uint(myVShard))
	}
	return myVShards
}
