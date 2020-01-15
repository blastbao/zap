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

package zap

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/blastbao/zap/zapcore"
)

const schemeFile = "file"

var (
	_sinkMutex     sync.RWMutex
	_sinkFactories map[string] func(*url.URL) (Sink, error) // keyed by scheme
)

func init() {
	resetSinkRegistry()
}

func resetSinkRegistry() {
	_sinkMutex.Lock()
	defer _sinkMutex.Unlock()

	_sinkFactories = map[string] func(*url.URL) (Sink, error) {
		schemeFile: newFileSink,
	}
}

// Sink defines the interface to write to and close logger destinations.
type Sink interface {
	zapcore.WriteSyncer
	io.Closer
}

// 定义 nopCloserSink 的目的是，当输出目标是 os.Stdout / os.Stderr 时，不能调用其本身的 Close() 函数，
// 有可能影响系统行为，所以需要 Hook() 掉。而对于普通的 os.File 则无需被 nopCloserSink 包一层。
type nopCloserSink struct{
	zapcore.WriteSyncer
}

func (nopCloserSink) Close() error {
	return nil
}



type errSinkNotFound struct {
	scheme string
}

func (e *errSinkNotFound) Error() string {
	return fmt.Sprintf("no sink found for scheme %q", e.scheme)
}

// RegisterSink registers a user-supplied factory for all sinks with a particular scheme.
//
// All schemes must be ASCII, valid under section 3.1 of RFC 3986 (https://tools.ietf.org/html/rfc3986#section-3.1),
// and must not already have a factory registered.
//
// Zap automatically registers a factory for the "file" scheme.
func RegisterSink(scheme string, factory func(*url.URL) (Sink, error)) error {

	_sinkMutex.Lock()
	defer _sinkMutex.Unlock()

	if scheme == "" {
		return errors.New("can't register a sink factory for empty string")
	}

	// 检查 scheme 是否符合要求
	normalized, err := normalizeScheme(scheme)
	if err != nil {
		return fmt.Errorf("%q is not a valid scheme: %v", scheme, err)
	}

	// 根据 normalized 获取 Sink ，如果已经存在，意味着此次是重复注册，则报错
	if _, ok := _sinkFactories[normalized]; ok {
		return fmt.Errorf("sink factory already registered for scheme %q", normalized)
	}

	// 若不存在，则注册
	_sinkFactories[normalized] = factory

	return nil
}

func newSink(rawURL string) (Sink, error) {
	// 解析 url
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("can't parse %q as a URL: %v", rawURL, err)
	}

	// 检查 u.Scheme，若为空则设置默认值 "file"
	if u.Scheme == "" {
		u.Scheme = schemeFile
	}

	// 从 _sinkFactories[] 中获取 u.Scheme 对应的 factory 函数，对于 "file" 类型的 schema 会返回 `newFileSink(u *url.URL) (Sink, error)`
	_sinkMutex.RLock()
	factory, ok := _sinkFactories[u.Scheme]
	_sinkMutex.RUnlock()
	if !ok {
		return nil, &errSinkNotFound{u.Scheme}
	}

	// factory(u) 返回 Sink，对于 "file" 类型会返回 *os.File，它实现了 Sink 接口
	return factory(u)
}


func newFileSink(u *url.URL) (Sink, error) {

	// 对于 file 类型的 url，不应该包含一些冗余参数，需要进行检查，
	// 要求 u.User、u.Fragment、u.RawQuery、u.Port()、hn := u.Hostname() 必须为空（或默认值）

	if u.User != nil {
		return nil, fmt.Errorf("user and password not allowed with file URLs: got %v", u)
	}

	if u.Fragment != "" {
		return nil, fmt.Errorf("fragments not allowed with file URLs: got %v", u)
	}

	if u.RawQuery != "" {
		return nil, fmt.Errorf("query parameters not allowed with file URLs: got %v", u)
	}

	// Error messages are better if we check hostname and port separately.
	if u.Port() != "" {
		return nil, fmt.Errorf("ports not allowed with file URLs: got %v", u)
	}

	if hn := u.Hostname(); hn != "" && hn != "localhost" {
		return nil, fmt.Errorf("file URLs must leave host empty or use localhost: got %v", u)
	}

	// 根据 u.Path 决定输出目的地
	switch u.Path {
	case "stdout":
		return nopCloserSink{os.Stdout}, nil
	case "stderr":
		return nopCloserSink{os.Stderr}, nil
	}

	// 对于 os.Stdout / os.Stderr 需要用 nopCloserSink 包一层以 Hook 掉 Close() 函数，
	// 避免影响标准输出/错误输出的处理，而对于普通的 os.File 则可以直接返回。
	return os.OpenFile(u.Path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
}

// 归一化
func normalizeScheme(s string) (string, error) {

	// https://tools.ietf.org/html/rfc3986#section-3.1

	// 小写
	s = strings.ToLower(s)

	// 首字符必须为字母
	if first := s[0]; 'a' > first || 'z' < first {
		return "", errors.New("must start with a letter")
	}

	// 检查是否包含非法字符，字母、数字、符号(./+/-)
	for i := 1; i < len(s); i++ { // iterate over bytes, not runes
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z':
			continue
		case '0' <= c && c <= '9':
			continue
		case c == '.' || c == '+' || c == '-':
			continue
		}
		return "", fmt.Errorf("may not contain %q", c)
	}

	// 返回 s
	return s, nil
}
