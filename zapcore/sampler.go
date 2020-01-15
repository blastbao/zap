// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package zapcore

import (
	"time"

	"go.uber.org/atomic"
)

const (
	_numLevels        = _maxLevel - _minLevel + 1   // 桶的总数 等于 日志级别总数 len(debug, ..., fatal)
	_countersPerLevel = 4096 						// 每个桶有 4096 个槽
)


type counter struct {
	resetAt atomic.Int64	//
	counter atomic.Uint64	// 当前 log entry 的重复次数
}

type counters [_numLevels][_countersPerLevel]counter

func newCounters() *counters {
	return &counters{}
}

func (cs *counters) get(lvl Level, key string) *counter {
	i := lvl - _minLevel 				 // 根据日志级别，确定桶号
	j := fnv32a(key) % _countersPerLevel // 哈希后取模，确定计数器槽号，相同内容的 log entry 会定位到同一个槽
	return &cs[i][j]
}

// fnv32a, adapted from "hash/fnv", but without a []byte(string) alloc
func fnv32a(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	hash := uint32(offset32)
	for i := 0; i < len(s); i++ {
		hash ^= uint32(s[i])
		hash *= prime32
	}
	return hash
}


// counter.IncCheckReset 方法接受两个参数：
// 	t: 日志条目中的时间
// 	tick: 日志限流的生效周期
//
// 此方法的作用是，在生效周期内，能够并发安全的累加调用次数，并返回当前是在生效周期内第 n 次调用该方法；
// 而如果超过了生效周期，能够重置生效周期，并把计数置为 1 后并返回。
func (c *counter) IncCheckReset(t time.Time, tick time.Duration) uint64 {

	// 转换成纳秒 tn
	tn := t.UnixNano()

	// 初始值是 0
	resetAfter := c.resetAt.Load()

	// 比较 t 和 resetAt 的大小，如果 t 还没超过 resetAt ，直接返回 counter 自增后的值
	if resetAfter > tn {
		// 重复次数 +1
		return c.counter.Inc()
	}

	// 如果 t 超过 resetAt ，意味着已经到达重置的时间，直接将 counter 重置为 1
	c.counter.Store(1)

	// 重新计算下次重置时间，即在 `日志条目纳秒时间` 基础上增加 tick
	newResetAfter := tn + tick.Nanoseconds()

	// cas 设置，如果设置失败意味着已经被设置了，直接返回当前计数值
	if !c.resetAt.CAS(resetAfter, newResetAfter) {
		// We raced with another goroutine trying to reset, and it also reset
		// the counter to 1, so we need to reincrement the counter.
		return c.counter.Inc()
	}

	// 如果设置成功，直接返回 1
	return 1
}


type sampler struct {

	//
	Core

	counts            *counters
	tick              time.Duration
	first, thereafter uint64
}

// NewSampler creates a Core that samples incoming entries, which caps the CPU
// and I/O load of logging while attempting to preserve a representative subset
// of your logs.
//
// Zap samples by logging the first N entries with a given level and message
// each tick. If more Entries with the same level and message are seen during
// the same interval, every Mth message is logged and the rest are dropped.
//
// Keep in mind that zap's sampling implementation is optimized for speed over
// absolute precision; under load, each tick may be slightly over- or
// under-sampled.
func NewSampler(core Core, tick time.Duration, first, thereafter int) Core {
	return &sampler{
		Core:       core,
		tick:       tick,
		counts:     newCounters(),
		first:      uint64(first),
		thereafter: uint64(thereafter),
	}
}

func (s *sampler) With(fields []Field) Core {
	return &sampler{
		Core:       s.Core.With(fields),
		tick:       s.tick,
		counts:     s.counts,
		first:      s.first,
		thereafter: s.thereafter,
	}
}

func (s *sampler) Check(ent Entry, ce *CheckedEntry) *CheckedEntry {

	// 检查日志级别，判断日志是否应该输出
	if !s.Enabled(ent.Level) {
		return ce
	}

	// 根据 `日志级别` 和 `日志信息` 从 s.counts 中获取到该日志对应的计数器
	counter := s.counts.get(ent.Level, ent.Message)

	// 在生效周期内，能够并发安全的累加，并返回当前是在生效周期内第 n 次调用该方法
	n := counter.IncCheckReset(ent.Time, s.tick)


	// 每隔 s.thereafter 输出一次
	if n > s.first && (n-s.first)%s.thereafter != 0 {
		return ce
	}

	//
	return s.Core.Check(ent, ce)
}
