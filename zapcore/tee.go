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

import "go.uber.org/multierr"



// multiCore 实现了 Core 接口，它可以在任何位置替换 Core
type multiCore []Core


// NewTee creates a Core that duplicates log entries into two or more underlying Cores.
//
// Calling it with a single Core returns the input unchanged, and calling it with no input returns a no-op Core.
//
// multiCore 存在的意义在于，一个 logger 上可以绑定多种不同的输出策略，
// 如不同级别的日志写入不同的文件，或者一些日志写本地文件，一部分日志写入远程的 kafka 等等。
//
// 这些不同的策略需要不同的 Core 来对编码方式、文件路径、级别控制等等做区分。
// 这个时候就需要用到 multiCore 了，multiCore 类型的定义实际是一个 Core 的切片。
//
// 在 multiCore 的实现中，几乎所有的成员方法都会把所包含的 Cores 遍历一遍，逐个处理。

func NewTee(cores ...Core) Core {
	switch len(cores) {
	case 0:
		return NewNopCore()
	case 1:
		return cores[0]
	default:
		return multiCore(cores)
	}
}

func (mc multiCore) With(fields []Field) Core {
	clone := make(multiCore, len(mc))
	for i := range mc {
		clone[i] = mc[i].With(fields)
	}
	return clone
}

func (mc multiCore) Enabled(lvl Level) bool {
	for i := range mc {
		if mc[i].Enabled(lvl) {
			return true
		}
	}
	return false
}

// Check 方法中会分别调用封装的 Cores 中的 Check 方法。
// 以 ioCore 为例，其 Check 方法会先通过 Enabled 方法检查是否应该输出，若应该便会把自己保存到 ce.cores 中 。
func (mc multiCore) Check(ent Entry, ce *CheckedEntry) *CheckedEntry {
	for i := range mc {
		ce = mc[i].Check(ent, ce)
	}
	return ce
}

func (mc multiCore) Write(ent Entry, fields []Field) error {
	var err error
	for i := range mc {
		err = multierr.Append(err, mc[i].Write(ent, fields))
	}
	return err
}

func (mc multiCore) Sync() error {
	var err error
	for i := range mc {
		err = multierr.Append(err, mc[i].Sync())
	}
	return err
}
