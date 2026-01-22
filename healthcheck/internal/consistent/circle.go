package consistent

// An implementation of Consistent Hashing and
// Consistent Hashing With Bounded Loads.
//
// https://en.wikipedia.org/wiki/Consistent_hashing
//
// https://research.googleblog.com/2017/04/consistent-hashing-with-bounded-loads.html

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"

	blake2b "github.com/minio/blake2b-simd"

	"github.com/Sh00ty/network-lb/health-check-node/internal/sharder"
)

const replicationFactor = 10

var ErrNoHosts = errors.New("no hosts added")

type Host struct {
	Name sharder.ShardingNodeID
	Load int64
}

type Consistent struct {
	hosts     map[uint64]sharder.ShardingNodeID
	sortedSet []uint64
	loadMap   map[sharder.ShardingNodeID]*Host
	totalLoad int64

	sync.RWMutex
}

func NewCirle() *Consistent {
	return &Consistent{
		hosts:     map[uint64]sharder.ShardingNodeID{},
		sortedSet: []uint64{},
		loadMap:   map[sharder.ShardingNodeID]*Host{},
	}
}

func (c *Consistent) MarkHealthy(host sharder.ShardingNodeID) error {
	c.Lock()
	defer c.Unlock()

	if _, ok := c.loadMap[host]; ok {
		return nil
	}

	c.loadMap[host] = &Host{Name: host, Load: 0}
	for i := range replicationFactor {
		h := c.hash(fmt.Sprintf("%d%d", host, i))
		c.hosts[h] = host
		c.sortedSet = append(c.sortedSet, h)

	}
	// sort hashes ascendingly
	sort.Slice(c.sortedSet, func(i int, j int) bool {
		if c.sortedSet[i] < c.sortedSet[j] {
			return true
		}
		return false
	})
	return nil
}

func (c *Consistent) GetWithOffset(key int64, offset uint) (sharder.ShardingNodeID, error) {
	c.RLock()
	defer c.RUnlock()

	if len(c.hosts) == 0 {
		return 0, ErrNoHosts
	}
	if uint(len(c.loadMap)) <= offset {
		return c.hosts[c.sortedSet[0]], nil
	}
	var (
		h          = c.hash(strconv.FormatInt(key, 10))
		idx        = c.search(h)
		sourseHost = c.hosts[c.sortedSet[idx]]
		seenHosts  = map[sharder.ShardingNodeID]struct{}{
			sourseHost: {},
		}
		candidate = sharder.ShardingNodeID(-1)
	)
	if offset == 0 {
		return sourseHost, nil
	}
	for k := offset; k > 0; {
		idx = (idx + 1) % len(c.sortedSet)
		candidate = c.hosts[c.sortedSet[idx]]
		if _, seen := seenHosts[candidate]; !seen {
			k--
			seenHosts[candidate] = struct{}{}
		}
	}
	return candidate, nil
}

func (c *Consistent) search(key uint64) int {
	idx := sort.Search(len(c.sortedSet), func(i int) bool {
		return c.sortedSet[i] >= key
	})

	if idx >= len(c.sortedSet) {
		idx = 0
	}
	return idx
}

func (c *Consistent) MarkUnhealthy(host sharder.ShardingNodeID) error {
	c.Lock()
	defer c.Unlock()

	for i := range replicationFactor {
		h := c.hash(fmt.Sprintf("%d%d", host, i))
		delete(c.hosts, h)
		c.delSlice(h)
	}
	delete(c.loadMap, host)
	return nil
}

func (c *Consistent) delSlice(val uint64) {
	idx := -1
	l := 0
	r := len(c.sortedSet) - 1
	for l <= r {
		m := (l + r) / 2
		if c.sortedSet[m] == val {
			idx = m
			break
		} else if c.sortedSet[m] < val {
			l = m + 1
		} else if c.sortedSet[m] > val {
			r = m - 1
		}
	}
	if idx != -1 {
		c.sortedSet = append(c.sortedSet[:idx], c.sortedSet[idx+1:]...)
	}
}

func (c *Consistent) hash(key string) uint64 {
	out := blake2b.Sum512([]byte(key))
	return binary.LittleEndian.Uint64(out[:])
}
