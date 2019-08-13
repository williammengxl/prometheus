// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package chunkenc

import (
	"fmt"
	"math"
	"sync"

	"github.com/pkg/errors"
)

// Encoding is the identifier for a chunk encoding.
type Encoding uint8

func (e Encoding) String() string {
	switch e {
	case EncNone:
		return "none"
	case EncXOR:
		return "XOR"
	}
	return "<unknown>"
}

// The different available chunk encodings.
const (
	EncNone Encoding = iota
	EncXOR
)

// Chunk holds a sequence of sample pairs that can be iterated over and appended to.
type Chunk interface {
	Bytes() []byte
	Encoding() Encoding
	Appender() (Appender, error)
	// The iterator passed as argument is for re-use.
	// Depending on implementation, the iterator can
	// be re-used or a new iterator can be allocated.
	Iterator(Iterator) Iterator
	NumSamples() int
}

// Appender adds sample pairs to a chunk.
type Appender interface {
	Append(int64, float64)
}

// Iterator iterates over the samples of a time series.
type Iterator interface {
	// Seek advances the iterator forward to the sample with the timestamp t or first value after t.
	// Seek has no effect if requested timestamp is the same or lower than the current iterator position.
	// Seek returns false if there is no such sample with the timestamp equal or larger than t.
	// Iterator is exhausted when the Seek returns false.
	Seek(t int64) bool
	// At returns the current timestamp/value pair.
	// At returns (math.MinInt64, 0.0) before the iterator has advanced.
	At() (int64, float64)
	// Next advances the iterator by one.
	Next() bool
	// Err returns the current error.
	Err() error
}

// NewNopIterator returns a new chunk iterator that does not hold any data.
func NewNopIterator() Iterator {
	return nopIterator{}
}

type nopIterator struct{}

func (nopIterator) Seek(t int64) bool    { return false }
func (nopIterator) At() (int64, float64) { return math.MinInt64, 0 }
func (nopIterator) Next() bool           { return false }
func (nopIterator) Err() error           { return nil }

// Pool is used to create and reuse chunk references to avoid allocations.
type Pool interface {
	Put(Chunk) error
	Get(e Encoding, b []byte) (Chunk, error)
}

// pool is a memory pool of chunk objects.
type pool struct {
	xor sync.Pool
}

// NewPool returns a new pool.
func NewPool() Pool {
	return &pool{
		xor: sync.Pool{
			New: func() interface{} {
				return &XORChunk{b: bstream{}}
			},
		},
	}
}

func (p *pool) Get(e Encoding, b []byte) (Chunk, error) {
	switch e {
	case EncXOR:
		c := p.xor.Get().(*XORChunk)
		c.b.stream = b
		c.b.count = 0
		return c, nil
	}
	return nil, errors.Errorf("invalid encoding %q", e)
}

func (p *pool) Put(c Chunk) error {
	switch c.Encoding() {
	case EncXOR:
		xc, ok := c.(*XORChunk)
		// This may happen often with wrapped chunks. Nothing we can really do about
		// it but returning an error would cause a lot of allocations again. Thus,
		// we just skip it.
		if !ok {
			return nil
		}
		xc.b.stream = nil
		xc.b.count = 0
		p.xor.Put(c)
	default:
		return errors.Errorf("invalid encoding %q", c.Encoding())
	}
	return nil
}

// FromData returns a chunk from a byte slice of chunk data.
// This is there so that users of the library can easily create chunks from
// bytes.
func FromData(e Encoding, d []byte) (Chunk, error) {
	switch e {
	case EncXOR:
		return &XORChunk{b: bstream{count: 0, stream: d}}, nil
	}
	return nil, fmt.Errorf("unknown chunk encoding: %d", e)
}