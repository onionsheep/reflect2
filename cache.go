package reflect2

import (
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"unsafe"
)

// cpu cacheline size 64 bytes = 64 * 8 = 512 bits

const AutoSizeMemoryMax = uintptr(1048576)

// entry
type entry struct {
	rtype uintptr
	type2 Type
}

// bucket
type bucket []entry

// typeCache1
type typeCache1 struct {
	hashOffset uintptr
	mask       uintptr
	table      []unsafe.Pointer
	_table     []*bucket // gc holder
}

func (m *typeCache1) Get(rtype uintptr) (ret Type) {
	var bktIdx = rtype >> m.hashOffset & m.mask
	var bkt = (*bucket)(atomic.LoadPointer(&m.table[bktIdx])) // bkt := (*bucket)(m.preloadTable[bktIdx])
	if bkt == nil {                                           // *bkt 一定不是nil，因为Add的时候会make一个新的
		return
	}
	var bktLen = len(*bkt)
	var e entry
	for i := 0; i < bktLen; i++ {
		e = (*bkt)[i]
		if e.rtype == rtype {
			ret = e.type2
			break
		}
	}
	return
}

func (m *typeCache1) AddIfAbsent(rtype uintptr, type2 Type) {
	bktIdx := rtype >> m.hashOffset & m.mask
	var oBkt *bucket
	var nBkt bucket
	oBkt = (*bucket)(atomic.LoadPointer(&m.table[bktIdx]))
	if oBkt == nil {
		nBkt = []entry{{rtype: rtype, type2: type2}}
		if atomic.CompareAndSwapPointer(&m.table[bktIdx], nil, unsafe.Pointer(&nBkt)) {
			return
		}
	}
	// m.preloadTable[bktIdx] 非空，或者在替换的过程中变成了非空
	for {
		oBkt = (*bucket)(atomic.LoadPointer(&m.table[bktIdx]))
		for i := 0; i < len(*oBkt); i++ {
			if (*oBkt)[i].rtype == rtype {
				return
			}
		}
		nBkt = append(*oBkt, entry{rtype: rtype, type2: type2})
		if atomic.CompareAndSwapPointer(&m.table[bktIdx], unsafe.Pointer(oBkt), unsafe.Pointer(&nBkt)) {
			return
		}
	}
}

func NewTypeCache1(hashOffset uintptr, tableSize uintptr) *typeCache1 {
	if tableSize%2 != 0 {
		panic("tableSize must be 2^n")
	}
	var table = make([]*bucket, tableSize)

	var ret = &typeCache1{
		table:      *(*[]unsafe.Pointer)(unsafe.Pointer(&table)),
		mask:       tableSize - 1,
		hashOffset: hashOffset,
		_table:     table,
	}

	return ret
}

func NewTypeCache1AutoSize() *typeCache1 {
	var hashOffset, size = probeTypeCacheOptions(AutoSizeMemoryMax)
	return NewTypeCache1(hashOffset, size)
}

type ProbeDataItem struct {
	Offset uintptr
	Size   uintptr
	Access uintptr
	Memory uintptr
}

type ProbeData []*ProbeDataItem

func (p *ProbeData) Len() int {
	return len(*p)
}

func (p *ProbeData) Less(i, j int) bool {
	if (*p)[i].Access < (*p)[j].Access {
		return true
	} else if (*p)[i].Access == (*p)[j].Access {
		return (*p)[i].Memory < (*p)[j].Memory
	}
	return false
}

func (p *ProbeData) Swap(i, j int) {
	t := (*p)[i]
	(*p)[i] = (*p)[j]
	(*p)[j] = t
}

func probeTypeCacheOptions(memMax uintptr) (uintptr, uintptr) {
	keys := getAllTypeKeys()

	offMin := uintptr(4)
	offMax := uintptr(48)
	sizeMin := uintptr(1)
	sizeMax := uintptr(1 << 20)
	keysLen := uintptr(len(keys))

	probeData := make(ProbeData, 0)

	for off := offMin; off < offMax; off++ {
		offCounter := calcOffsetCounter(keys, off)
		// 当由于位移导致的重复超过半数的时候，跳出循环
		if uintptr(len(offCounter)) <= keysLen>>1 {
			continue
		}
		for size := sizeMin; size <= sizeMax; size = size << 1 {
			bucketLen := calcBucketsLen(size, keys, off)
			mem := calcMemoryForCache1(size, bucketLen)
			accessScore := calculateAccessScoreForCache1(bucketLen)
			if memMax != 0 && mem > memMax {
				break
			}
			probeData = append(probeData, &ProbeDataItem{
				Offset: off,
				Size:   size,
				Access: accessScore,
				Memory: mem,
			})
		}
	}
	sort.Sort(&probeData)
	for _, d := range probeData {
		log.Printf("A=%7d, M=%7d, S=%7d, O=%2d", d.Access, d.Memory, d.Size, d.Offset)
	}

	return probeData[0].Offset, probeData[0].Size
}

func calculateAccessScoreForCache1(bucketLen map[uintptr]uintptr) uintptr {
	accessScore := uintptr(0)     // 如果没有cache，那么就是需要访存的次数
	for _, l := range bucketLen { // bucketLen[index]length
		accessScore += 1
		for i := uintptr(0); i < l; i++ {
			accessScore += i
		}
	}
	return accessScore
}

func calcMemoryForCache1(size uintptr, bucketLen map[uintptr]uintptr) uintptr {
	mem := unsafe.Sizeof(typeCache2{})               // header
	mem += size * unsafe.Sizeof(unsafe.Pointer(nil)) // preloadTable
	for _, length := range bucketLen {               // bucketLen[index]length
		if length <= 2 {
			mem += length * unsafe.Sizeof(entry{})
		} else {
			mem += length*unsafe.Sizeof(entry{}) + unsafe.Sizeof([]entry{})
		}
	}
	return mem
}

func calcBucketsLen(size uintptr, keys []uintptr, off uintptr) map[uintptr]uintptr {
	bucketLen := make(map[uintptr]uintptr)
	mask := size - 1
	for _, key := range keys {
		index := (key >> off) & mask
		if v, ok := bucketLen[index]; ok {
			bucketLen[index] = v + 1
		} else {
			bucketLen[index] = uintptr(1)
		}
	}
	return bucketLen
}

func calcOffsetCounter(keys []uintptr, off uintptr) map[uintptr]uintptr {
	offCounter := make(map[uintptr]uintptr)
	for _, key := range keys {
		index := key >> off
		if v, ok := offCounter[index]; ok {
			offCounter[index] = v + 1
		} else {
			offCounter[index] = uintptr(1)
		}
	}
	return offCounter
}

func getAllTypeKeys() []uintptr {
	initOnce.Do(discoverTypes) // trigger initOnce.Do(discoverTypes)

	keys := make([]uintptr, 0, len(types))
	for _, type1 := range types {
		cacheKey := uintptr(unpackEFace(type1).data)
		keys = append(keys, cacheKey)
	}
	return keys
}

func Probe() {
	_offset, _size := probeTypeCacheOptions(0)
	log.Printf("types.len=%v, probe.offset=%v, probe.size=%v", len(types), _offset, _size)
}

type typeCache2 struct {
	mask       uintptr
	hashOffset uintptr
	table      []unsafe.Pointer
	_table     []*bucket2 // gc holder
}

type bucket2 struct {
	entry entry   // size 1 * 24 = 24 bytes
	extra []entry // size 1 * 24 = 24 bytes
}

func (m *typeCache2) Get(rtype uintptr) (type2 Type) {
	var index = rtype >> m.hashOffset & m.mask

	var bkt = (*bucket2)(atomic.LoadPointer(&m.table[index])) // bkt := (*bucket)(m.preloadTable[bktIdx])
	if bkt == nil {                                           // *bkt 一定不是nil，因为Add的时候会make一个新的
		return nil
	}
	if bkt.entry.rtype == 0 {
		return nil
	}
	if bkt.entry.rtype == rtype {
		return bkt.entry.type2
	}
	bktExtLen := len(bkt.extra)
	for i := 0; i < bktExtLen; i++ {
		var e = (bkt.extra)[i]
		if e.rtype == rtype {
			type2 = e.type2
			break
		}
	}
	return
}

func (m *typeCache2) AddIfAbsent(rtype uintptr, type2 Type) {
	var index = rtype >> m.hashOffset & m.mask
	var oBkt *bucket2
	var nBkt bucket2
	oBkt = (*bucket2)(atomic.LoadPointer(&m.table[index]))
	if oBkt == nil {
		nBkt = bucket2{
			entry: entry{rtype: rtype, type2: type2},
			extra: nil,
		}
		if atomic.CompareAndSwapPointer(&m.table[index], nil, unsafe.Pointer(&nBkt)) {
			return
		}
	}

	// table[index] 非空，或者在替换的过程中变成了非空
	for {
		oBkt = (*bucket2)(atomic.LoadPointer(&m.table[index]))
		if oBkt.entry.rtype == rtype {
			return
		} else if oBkt.extra == nil {
			nBkt = bucket2{
				entry: oBkt.entry,
				extra: []entry{{rtype: rtype, type2: type2}},
			}
		} else {
			nBkt = bucket2{
				entry: oBkt.entry,
				extra: append(oBkt.extra, entry{rtype: rtype, type2: type2}),
			}
		}
		if atomic.CompareAndSwapPointer(&m.table[index], unsafe.Pointer(oBkt), unsafe.Pointer(&nBkt)) {
			return
		}
	}
}

func NewTypeCache2(hashOffset uintptr, tableSize uintptr) *typeCache2 {
	if tableSize%2 != 0 {
		panic("tableSize must be 2^n")
	}
	var table = make([]*bucket2, tableSize)

	var ret = &typeCache2{
		mask:       tableSize - 1,
		hashOffset: hashOffset,
		table:      *(*[]unsafe.Pointer)(unsafe.Pointer(&table)),
		_table:     table,
	}

	return ret
}

func (m *typeCache2) preload() {
	for _, type1 := range types {
		type2 := ConfigUnsafe.wrapType(type1)
		rtype := type2.RType()
		m.AddIfAbsent(rtype, type2)
	}
}

func NewTypeCache2AutoSize() *typeCache2 {
	var hashOffset, size = probeTypeCacheOptions(AutoSizeMemoryMax)
	return NewTypeCache2(hashOffset, size)
}

type typeCache3 struct {
	mask         uintptr   // 8 byte
	hashOffset   uintptr   // 8 byte
	preloadTable []entry   // 24 byte, preload types. real type is [N]entry, N can be 2^n.
	mp           *sync.Map // 8 byte, key rtype, value type2
}

func NewTypeCache3(hashOffset, size uintptr) *typeCache3 {
	if size%2 != 0 {
		panic("tableSize must be 2^n")
	}
	preloadTable := make([]entry, size)
	m := &typeCache3{
		mask:         size - 1,
		hashOffset:   hashOffset,
		preloadTable: preloadTable,
		mp:           &sync.Map{},
	}
	return m
}

func NewTypeCache3AutoSize() *typeCache3 {
	offset, size := probeTypeCacheOptions(AutoSizeMemoryMax)
	return NewTypeCache3(offset, size)
}

func (m *typeCache3) preload() {
	for _, type1 := range types {
		type2 := ConfigUnsafe.wrapType(type1)
		rtype := type2.RType()
		index := rtype >> m.hashOffset & m.mask
		m.preloadTable[index].rtype = rtype
		m.preloadTable[index].type2 = type2
	}
}

func (m *typeCache3) Get(rtype uintptr) (type2 Type) {
	index := rtype >> m.hashOffset & m.mask
	e := m.preloadTable[index]
	if e.rtype == rtype {
		return e.type2
	}

	if v, ok := m.mp.Load(rtype); ok {
		return v.(Type)
	}
	return nil
}

func (m *typeCache3) AddIfAbsent(rtype uintptr, type2 Type) {
	m.mp.Store(rtype, type2)
}
