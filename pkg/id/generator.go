package id

import (
	"io"
	"sync"
	"time"
)

const maxSeq = 0xFFF

type Clock interface {
	Now() time.Time
}

type Generator interface {
	NewID() ID
}

var (
	_ Generator = (*V7)(nil)
	_ Generator = (*Sequence)(nil)
)

type V7 struct {
	mu      sync.Mutex
	clock   Clock
	entropy io.Reader
	lastMS  int64
	seq     uint16
}

func NewV7(c Clock, entropy io.Reader) *V7 {
	if c == nil {
		panic("id: NewV7 with a nil clock")
	}
	if entropy == nil {
		panic("id: NewV7 with a nil entropy source")
	}
	return &V7{clock: c, entropy: entropy}
}

func (g *V7) NewID() ID {
	ms, seq := g.tick()

	var rand [8]byte
	if _, err := io.ReadFull(g.entropy, rand[:]); err != nil {
		panic("id: entropy source failed: " + err.Error())
	}

	var out ID
	putMilli(&out, ms)
	out[6] = 0x70 | byte(seq>>8)
	out[7] = byte(seq)
	copy(out[8:], rand[:])
	out[8] = 0x80 | (out[8] & 0x3F)
	return out
}

func (g *V7) tick() (milli int64, seq uint16) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock.Now().UnixMilli()
	switch {
	case now > g.lastMS:
		g.lastMS, g.seq = now, 0
	case g.seq < maxSeq:
		g.seq++
	default:
		g.lastMS++
		g.seq = 0
	}
	return g.lastMS, g.seq
}

type Sequence struct {
	mu sync.Mutex
	at time.Time
	n  uint64
}

func NewSequence(at time.Time) *Sequence { return &Sequence{at: at} }

func (s *Sequence) NewID() ID {
	s.mu.Lock()
	n := s.n
	s.n++
	s.mu.Unlock()

	var out ID
	putMilli(&out, s.at.UnixMilli())
	out[6] = 0x70
	out[7] = 0
	out[8] = 0x80 | byte((n>>56)&0x3F)
	for j := 0; j < 7; j++ {
		out[9+j] = byte(n >> (8 * (6 - j)))
	}
	return out
}

func putMilli(out *ID, ms int64) {
	out[0] = byte(ms >> 40)
	out[1] = byte(ms >> 32)
	out[2] = byte(ms >> 24)
	out[3] = byte(ms >> 16)
	out[4] = byte(ms >> 8)
	out[5] = byte(ms)
}
