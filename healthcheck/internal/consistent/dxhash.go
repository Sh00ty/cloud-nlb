package consistent

import (
	"fmt"
	"math/rand/v2"
	"sync"

	"github.com/Sh00ty/network-lb/health-check-node/internal/sharder"
)

type NodeState int8

const (
	empty NodeState = iota
	Unhealthy
	Healthy
)

type DxHash struct {
	searchLimit int
	nsArray     []NodeState
	maxWeight   uint64
	weights     []int16
	mu          sync.RWMutex

	prngPool sync.Pool
}

type pairedPRNG struct {
	nodePrng   Prng
	weightPrng Prng
}

// Предоставляет равномерное распределение на целых положительных числах
type Prng interface {
	Seed(seed1 uint64, seed2 uint64)
	Uint64() uint64
}

func New(nodeCount int, options ...Option) *DxHash {
	option := option{
		// по дефолту удобно мыслить в терминах процентов канарейки
		maxWeight: 100,
		// в публикации говорится, что searchLimit в идеале = 8 * nodeCount
		searchLimit: 8 * nodeCount,
		// в публикации советуется использовать ближайшую степень двойки
		// на деле лучше сделать запас побольше
		nsArrLen:     findNearestTwoDegree(int(nodeCount)),
		initialState: Healthy,
		newPrng: func() Prng {
			return rand.NewPCG(0, 0)
		},
	}
	for _, opt := range options {
		opt(&option)
	}
	nsArray := make([]NodeState, option.nsArrLen)
	for i := range nodeCount {
		nsArray[i] = option.initialState
	}
	if len(option.weights) != 0 {
		option.weights = append(option.weights, make([]int16, option.nsArrLen-len(option.weights))...)
	}
	return &DxHash{
		searchLimit: option.searchLimit,
		nsArray:     nsArray,
		maxWeight:   uint64(option.maxWeight),
		weights:     option.weights,
		prngPool: sync.Pool{
			New: func() any {
				return &pairedPRNG{
					nodePrng:   option.newPrng(),
					weightPrng: option.newPrng(),
				}
			},
		},
	}
}

func (dx *DxHash) isWeighted() bool {
	return len(dx.weights) != 0
}

func (dx *DxHash) Get(key int64) (sharder.ShardingNodeID, error) {
	return dx.getNthNode(key, 0)
}

func (dx *DxHash) GetWithOffset(key int64, backupNum uint) (sharder.ShardingNodeID, error) {
	return dx.getNthNode(key, backupNum)
}

func (dx *DxHash) getNthNode(key int64, offset uint) (sharder.ShardingNodeID, error) {
	prngs := dx.prngPool.Get().(*pairedPRNG)
	defer dx.prngPool.Put(prngs)

	prngs.nodePrng.Seed(uint64(key), 0)
	if dx.isWeighted() {
		prngs.weightPrng.Seed(uint64(key), uint64(dx.searchLimit))
	}

	dx.mu.RLock()
	defer dx.mu.RUnlock()

	seenHosts := map[sharder.ShardingNodeID]struct{}{}

	needSkip := offset
	for range dx.searchLimit {
		var (
			nodeID                 = prngs.nodePrng.Uint64() % uint64(len(dx.nsArray))
			weightChooseThreashold = int16(0)
		)
		if dx.isWeighted() {
			weightChooseThreashold = int16(prngs.weightPrng.Uint64() % dx.maxWeight)
		}
		if dx.nsArray[nodeID] != Healthy {
			continue
		}
		_, seen := seenHosts[sharder.ShardingNodeID(nodeID)]
		if !dx.isWeighted() {
			if needSkip != 0 && !seen {
				needSkip--
				seenHosts[sharder.ShardingNodeID(nodeID)] = struct{}{}
				continue
			}
			return sharder.ShardingNodeID(nodeID), nil
		}
		if weightChooseThreashold < dx.weights[nodeID] {
			if needSkip != 0 && !seen {
				needSkip--
				seenHosts[sharder.ShardingNodeID(nodeID)] = struct{}{}
				continue
			}
			return sharder.ShardingNodeID(nodeID), nil
		}
	}
	return dx.lockedGetFirstHelthyNode()
}

func (dx *DxHash) lockedGetFirstHelthyNode() (sharder.ShardingNodeID, error) {
	for nodeID, state := range dx.nsArray {
		if state == Healthy {
			return sharder.ShardingNodeID(nodeID), nil
		}
	}
	return 0, fmt.Errorf("failed to find healthy nodes")
}

func (dx *DxHash) MarkUnhealthy(nodeID sharder.ShardingNodeID) error {
	dx.mu.Lock()
	defer dx.mu.Unlock()

	if len(dx.nsArray) <= int(nodeID) {
		return fmt.Errorf("invalid node id")
	}
	curStatus := dx.nsArray[nodeID]
	if curStatus == empty {
		return fmt.Errorf("node id slot is empty")
	}
	dx.nsArray[nodeID] = Unhealthy
	return nil
}

func (dx *DxHash) MarkHealthy(nodeID sharder.ShardingNodeID) error {
	dx.mu.Lock()
	defer dx.mu.Unlock()

	if len(dx.nsArray) <= int(nodeID) {
		return fmt.Errorf("invalid node id")
	}
	curStatus := dx.nsArray[nodeID]
	if curStatus == empty {
		return fmt.Errorf("node id slot is empty")
	}
	dx.nsArray[nodeID] = Healthy
	return nil
}

func (dx *DxHash) ChangeWeight(nodeID sharder.ShardingNodeID, weight int16) error {
	dx.mu.Lock()
	defer dx.mu.Unlock()

	if !dx.isWeighted() {
		return fmt.Errorf("can't change weight in non weighted mode")
	}
	if len(dx.nsArray) <= int(nodeID) {
		return fmt.Errorf("invalid node id")
	}
	curStatus := dx.nsArray[nodeID]
	if curStatus == empty {
		return fmt.Errorf("node id slot is empty")
	}
	dx.weights[nodeID] = weight
	return nil
}

func (dx *DxHash) AddNode(weight int16, initialState NodeState) sharder.ShardingNodeID {
	dx.mu.Lock()
	defer dx.mu.Unlock()

	for i, nodeStatus := range dx.nsArray {
		if nodeStatus != empty {
			continue
		}
		dx.nsArray[i] = initialState
		if dx.isWeighted() {
			dx.weights[i] = weight
		}
		return sharder.ShardingNodeID(i)
	}
	oldNsArrayLen := len(dx.nsArray)
	dx.nsArray = append(dx.nsArray, make([]NodeState, oldNsArrayLen*2)...)
	dx.nsArray[oldNsArrayLen] = initialState
	if dx.isWeighted() {
		dx.weights = append(dx.weights, make([]int16, oldNsArrayLen*2)...)
		dx.weights[oldNsArrayLen] = weight
	}
	return sharder.ShardingNodeID(oldNsArrayLen)
}

func findNearestTwoDegree(i int) int {
	i--
	i |= i >> 1
	i |= i >> 2
	i |= i >> 4
	i |= i >> 8
	i |= i >> 16
	i++
	return i
}
