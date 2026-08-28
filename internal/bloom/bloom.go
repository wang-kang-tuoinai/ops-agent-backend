package bloom

import (
	"hash/fnv"
	"sync"
)

type BloomFilter struct {
	mu      sync.RWMutex
	bits    []byte
	size    uint32
	hashNum uint32
}

// TODO 持久化布隆过滤器
func NewBloomFilter(size uint32, hashNum uint32) *BloomFilter {
	return &BloomFilter{
		bits:    make([]byte, (size+7)/8),
		size:    size,
		hashNum: hashNum,
	}
}

func (b *BloomFilter) setBit(pos uint32) {
	byteIntex := pos / 8
	byteOffset := pos % 8
	b.bits[byteIntex] |= 1 << byteOffset
}

func (b *BloomFilter) getBit(pos uint32) bool {
	byteIntex := pos / 8
	byteOffset := pos % 8
	return b.bits[byteIntex]&(1<<byteOffset) != 0
}

func (b *BloomFilter) hash(item string, seed uint32) uint32 {
	h := fnv.New32a()
	h.Write([]byte(item))
	h.Write([]byte{byte(seed)})
	return h.Sum32() % b.size
}

func (b *BloomFilter) Add(item string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := uint32(0); i < b.hashNum; i++ {
		b.setBit(b.hash(item, i))
	}
}

func (b *BloomFilter) MightContain(item string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i := uint32(0); i < b.hashNum; i++ {
		if !b.getBit(b.hash(item, i)) {
			return false
		}
	}
	return true
}
