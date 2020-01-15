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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blastbao/zap/internal/bufferpool"
	"github.com/blastbao/zap/internal/exit"

	"go.uber.org/multierr"
)

var (
	_cePool = sync.Pool{New: func() interface{} {
		// Pre-allocate some space for cores.
		return &CheckedEntry{
			cores: make([]Core, 4),
		}
	}}
)


// 从对象池中获取可用的 CheckedEntry 的结构指针对象
func getCheckedEntry() *CheckedEntry {
	// 从 _cePool 中获取一个可用的 CheckedEntry 的结构指针对象
	ce := _cePool.Get().(*CheckedEntry)
	// 由于对象池中的对象会复用，调用 reset 清除脏数据
	ce.reset()

	return ce
}

func putCheckedEntry(ce *CheckedEntry) {
	if ce == nil {
		return
	}
	_cePool.Put(ce)
}

// NewEntryCaller makes an EntryCaller from the return signature of
// runtime.Caller.
func NewEntryCaller(pc uintptr, file string, line int, ok bool) EntryCaller {
	if !ok {
		return EntryCaller{}
	}
	return EntryCaller{
		PC:      pc,
		File:    file,
		Line:    line,
		Defined: true,
	}
}

// EntryCaller represents the caller of a logging function.
type EntryCaller struct {
	Defined bool
	PC      uintptr
	File    string
	Line    int
}

// String returns the full path and line number of the caller.
func (ec EntryCaller) String() string {
	return ec.FullPath()
}

// FullPath returns a /full/path/to/package/file:line description of the
// caller.
func (ec EntryCaller) FullPath() string {
	if !ec.Defined {
		return "undefined"
	}
	buf := bufferpool.Get()
	buf.AppendString(ec.File)
	buf.AppendByte(':')
	buf.AppendInt(int64(ec.Line))
	caller := buf.String()
	buf.Free()
	return caller
}

// TrimmedPath returns a package/file:line description of the caller,
// preserving only the leaf directory name and file name.
func (ec EntryCaller) TrimmedPath() string {
	if !ec.Defined {
		return "undefined"
	}
	// nb. To make sure we trim the path correctly on Windows too, we
	// counter-intuitively need to use '/' and *not* os.PathSeparator here,
	// because the path given originates from Go stdlib, specifically
	// runtime.Caller() which (as of Mar/17) returns forward slashes even on
	// Windows.
	//
	// See https://github.com/golang/go/issues/3335
	// and https://github.com/golang/go/issues/18151
	//
	// for discussion on the issue on Go side.
	//
	// Find the last separator.
	//
	idx := strings.LastIndexByte(ec.File, '/')
	if idx == -1 {
		return ec.FullPath()
	}
	// Find the penultimate separator.
	idx = strings.LastIndexByte(ec.File[:idx], '/')
	if idx == -1 {
		return ec.FullPath()
	}
	buf := bufferpool.Get()
	// Keep everything after the penultimate separator.
	buf.AppendString(ec.File[idx+1:])
	buf.AppendByte(':')
	buf.AppendInt(int64(ec.Line))
	caller := buf.String()
	buf.Free()
	return caller
}

// An Entry represents a complete log message. The entry's structured context
// is already serialized, but the log level, time, message, and call site
// information are available for inspection and modification.
//
// Entries are pooled, so any functions that accept them MUST be careful not to
// retain references to them.
type Entry struct {
	Level      Level
	Time       time.Time
	LoggerName string
	Message    string
	Caller     EntryCaller
	Stack      string
}

// CheckWriteAction indicates what action to take after a log entry is processed.
// Actions are ordered in increasing severity.
type CheckWriteAction uint8


const (
	// WriteThenNoop indicates that nothing special needs to be done.
	// It's the default behavior.
	WriteThenNoop CheckWriteAction = iota
	// WriteThenPanic causes a panic after Write.
	WriteThenPanic
	// WriteThenFatal causes a fatal os.Exit after Write.
	WriteThenFatal
)



// CheckedEntry is an Entry together with a collection of Cores that have already agreed to log it.
//
// CheckedEntry references should be created by calling AddCore or Should on a nil *CheckedEntry.
// References are returned to a pool after Write, and MUST NOT be retained after calling their Write method.
type CheckedEntry struct {
	Entry
	ErrorOutput WriteSyncer
	dirty       bool // best-effort detection of pool misuse
	should      CheckWriteAction
	cores       []Core
}


func (ce *CheckedEntry) reset() {
	// 重置 ce.Entry
	ce.Entry = Entry{}
	// 重置 ce.ErrorOutput
	ce.ErrorOutput = nil
	// dirty 是用来标识该 CheckedEntry 是不是一个脏数据，置为 false
	ce.dirty = false
	//
	ce.should = WriteThenNoop
	// 一个 CheckedEntry 上可能绑定多个不同的 cores ，这里把所有的 cores 都置空，并使切片长度归零。
	for i := range ce.cores {
		// don't keep references to cores
		ce.cores[i] = nil
	}
	ce.cores = ce.cores[:0]
}

// Write writes the entry to the stored Cores, returns any errors,
// and returns the CheckedEntry reference to a pool for immediate re-use.
// Finally, it executes any required CheckWriteAction.
func (ce *CheckedEntry) Write(fields ...Field) {

	// 参数检查
	if ce == nil {
		return
	}

	// 脏数据检查
	//
	// 正常情况下，通过 getCheckedEntry() 获取 CheckedEntry 时，一定调用过 reset 方法，ce.dirty 不应该为 true 。
	// 这里如果是 true ，说明 zap 内部发生了一些错误，或者是 zap 自身的 bug ，此时不可以输出正常日志的，需要写系统错误日志记录这一异常。
	if ce.dirty {
		// 写系统错误日志
		if ce.ErrorOutput != nil {
			// Make a best effort to detect unsafe re-use of this CheckedEntry.
			// If the entry is dirty, log an internal error; because the
			// CheckedEntry is being used after it was returned to the pool,
			// the message may be an amalgamation from multiple call sites.
			fmt.Fprintf(ce.ErrorOutput, "%v Unsafe CheckedEntry re-use near Entry %+v.\n", time.Now(), ce.Entry)
			ce.ErrorOutput.Sync()
		}
		return
	}

	// 因为当前 CheckedEntry 正在处理，为避免被错误重用，需要置 ce.dirty 为 true。
	//
	// 这里多啰嗦一点，如果严格使用对象池，这个 dirty 字段一般没有用处，除非 zap 库 `使用者` 或者 `二次开发者` 把 CheckedEntry 自行持有并多次使用，才有可能发生这种冲突。
	ce.dirty = true

	// 遍历 ce.cores ，逐个调用 ce.cores[i].Write() 函数，以将 ce.Entry 和 fields 写入目标地址，并汇总错误信息到 err 中。
	//
	// 这里用到 uber 自研的 multierr 包，可以将多个 error 拼接成一个，对于循环调用某些方法，最终判断有没有发生过错误的场景很实用。
	var err error
	for i := range ce.cores {
		err = multierr.Append(err, ce.cores[i].Write(ce.Entry, fields))
	}

	// 如果 err 不为 nil ，则把汇总后的错误信息写到错误输出中
	if ce.ErrorOutput != nil {
		if err != nil {
			fmt.Fprintf(ce.ErrorOutput, "%v write error: %v\n", time.Now(), err)
			ce.ErrorOutput.Sync()
		}
	}

	// 获取 ce.should 和 ce.Message 字段
	should, msg := ce.should, ce.Message

	// 至此，ce 使用完毕， 将其放回对象池中，以备下次使用
	putCheckedEntry(ce)

	// 判断了 should 的值，默认为 WriteThenNoop ，即写完不做任何操作；
	// 但对于 Panic 和 Fatal 级别的日志，分别需要 `调用 panic 方法` 或者 `进程直接无条件退出`。
	switch should {
	case WriteThenPanic:
		panic(msg)
	case WriteThenFatal:
		exit.Exit()
	}

}

// AddCore adds a Core that has agreed to log this CheckedEntry. It's intended to be
// used by Core.Check implementations, and is safe to call on nil CheckedEntry
// references.
func (ce *CheckedEntry) AddCore(ent Entry, core Core) *CheckedEntry {

	// 先判断了 reciever 是否为空，如果是空，就通过 getCheckedEntry() 获取一个可用 CheckedEntry 的结构体指针。
	if ce == nil {
		// 从 _cePool 中获取一个 CheckedEntry 的结构指针，这里使用对象池以减少 GC 压力。
		ce = getCheckedEntry()
		// 重新设置 entry
		ce.Entry = ent
	}

	// 添加新的 core
	ce.cores = append(ce.cores, core)

	return ce
}

// Should sets this CheckedEntry's CheckWriteAction, which controls whether a
// Core will panic or fatal after writing this log entry. Like AddCore, it's
// safe to call on nil CheckedEntry references.
func (ce *CheckedEntry) Should(ent Entry, should CheckWriteAction) *CheckedEntry {
	if ce == nil {
		ce = getCheckedEntry()
		ce.Entry = ent
	}
	ce.should = should
	return ce
}
