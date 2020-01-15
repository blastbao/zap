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
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/blastbao/zap/zapcore"
)

//A Logger provides fast, leveled, structured logging. All methods are safe
//for concurrent use.
//
//The Logger is designed for contexts in which every microsecond and every
//allocation matters, so its API intentionally favors performance and type
//safety over brevity. For most applications, the SugaredLogger strikes a
//better balance between performance and ergonomics.

// Logger 提供快速、分层、结构化的日志记录，所有方法都是并发安全的。
// 日志记录器是为对性能锱铢必较的应用场景而设计的，因此它的 API 有意地倾向于性能和类型安全，而不是简洁。
// 对于大多数应用程序来说，SugaredLogger 在性能和易用性之间取得更好的平衡。

type Logger struct {

	// core 是 Logger 的核心成员， 默认为 ioCore，
	core zapcore.Core

	// 开发者模式
	development bool

	// logger name
	name        string

	// 日志组件中出现异常时的输出
	errorOutput zapcore.WriteSyncer

	// 在日志输出内容里增加行号和文件名
	addCaller bool

	// 对指定的日志等级增加调用栈输出能力
	addStack  zapcore.LevelEnabler

	// 指定在调用栈中跳过的调用深度
	callerSkip int
}

// New constructs a new Logger from the provided zapcore.Core and Options.
// If the passed zapcore.Core is nil, it falls back to using a no-op implementation.
//
// This is the most flexible way to construct a Logger, but also the most verbose.
// For typical use cases, the highly-opinionated presets (NewProduction, NewDevelopment, and NewExample)
// or the Config struct are more convenient.
//
// For sample code, see the package-level AdvancedConfiguration example.
func New(core zapcore.Core, options ...Option) *Logger {

	// 如果 core 为 nil 则创建 NopLogger 并返回
	if core == nil {
		return NewNop()
	}

	// 构造 Logger
	log := &Logger{
		core:        core,
		errorOutput: zapcore.Lock(os.Stderr),  	// zap 内部错误输出到 stdErr
		addStack:    zapcore.FatalLevel + 1, 	// 对指定的日志等级增加调用栈输出能力
	}

	// 在 logger 上应用各个 options
	return log.WithOptions(options...)
}

// NewNop returns a no-op Logger. It never writes out logs or internal errors,
// and it never runs user-defined hooks.
//
// Using WithOptions to replace the Core or error output of a no-op Logger can
// re-enable logging.
//
//
func NewNop() *Logger {
	return &Logger{
		core:        zapcore.NewNopCore(),
		errorOutput: zapcore.AddSync(ioutil.Discard),
		addStack:    zapcore.FatalLevel + 1,
	}
}



// NewProduction builds a sensible production Logger that writes InfoLevel and
// above logs to standard error as JSON.
//
// It's a shortcut for NewProductionConfig().Build(...Option).
func NewProduction(options ...Option) (*Logger, error) {
	return NewProductionConfig().Build(options...)
}

// NewDevelopment builds a development Logger that writes DebugLevel and above
// logs to standard error in a human-friendly format.
//
// It's a shortcut for NewDevelopmentConfig().Build(...Option).
func NewDevelopment(options ...Option) (*Logger, error) {
	return NewDevelopmentConfig().Build(options...)
}

// NewExample builds a Logger that's designed for use in zap's testable
// examples. It writes DebugLevel and above logs to standard out as JSON, but
// omits the timestamp and calling function to keep example output
// short and deterministic.
func NewExample(options ...Option) *Logger {
	encoderCfg := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		NameKey:        "logger",
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}
	core := zapcore.NewCore(zapcore.NewJSONEncoder(encoderCfg), os.Stdout, DebugLevel)
	return New(core).WithOptions(options...)
}

//Sugar wraps the Logger to provide a more ergonomic, but slightly slower,
//API. Sugaring a Logger is quite inexpensive, so it's reasonable for a
//single application to use both Loggers and SugaredLoggers, converting
//between them on the boundaries of performance-sensitive code.

// Sugar 封装了日志记录器，以提供更符合人体工程学的、但速度稍慢的 API 。
// 对日志记录器进行糖化非常便宜，因此对于单个应用程序来说，同时使用日志记录器和糖化日志记录器是合理的，
// 可以在性能敏感代码的边界上在两者之间进行转换。

func (log *Logger) Sugar() *SugaredLogger {
	core := log.clone()
	core.callerSkip += 2
	return &SugaredLogger{core}
}

// Named adds a new path segment to the logger's name.
// Segments are joined by periods. By default, Loggers are unnamed.

func (log *Logger) Named(s string) *Logger {

	if s == "" {
		return log
	}

	l := log.clone()
	if log.name == "" {
		l.name = s
	} else {
		l.name = strings.Join([]string{l.name, s}, ".")
	}
	return l
}


// WithOptions clones the current Logger, applies the supplied Options, and returns the resulting Logger.
// It's safe to use concurrently.
func (log *Logger) WithOptions(opts ...Option) *Logger {
	c := log.clone()
	for _, opt := range opts {
		opt.apply(c)
	}
	return c
}



// With creates a child logger and adds structured context to it.
// Fields added to the child don't affect the parent, and vice versa.
func (log *Logger) With(fields ...Field) *Logger {
	if len(fields) == 0 {
		return log
	}
	l := log.clone()
	l.core = l.core.With(fields)
	return l
}




// Check returns a CheckedEntry if logging a message at the specified level is enabled.
// It's a completely optional optimization; in high-performance applications,
// Check can help avoid allocating a slice to hold fields.
func (log *Logger) Check(lvl zapcore.Level, msg string) *zapcore.CheckedEntry {
	return log.check(lvl, msg)
}





// Debug logs a message at DebugLevel.
// The message includes any fields passed at the log site,
// as well as any fields accumulated on the logger.
func (log *Logger) Debug(msg string, fields ...Field) {
	if ce := log.check(DebugLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Info logs a message at InfoLevel.
// The message includes any fields passed at the log site,
// as well as any fields accumulated on the logger.
func (log *Logger) Info(msg string, fields ...Field) {
	// log.check() 检查 InfoLevel 级别日志是否应该输出，如果应该则会返回 CheckedEntry 结构体 ce，ce 中包含了需要输出到文件的信息。
	if ce := log.check(InfoLevel, msg); ce != nil {
		// 遍历 ce.cores 逐个调用 ce.cores[i].Write(ce.Entry, fields...) 函数，以将 Entry 和 fields 写入多个目标文件中。
		ce.Write(fields...)
	}
}

// Warn logs a message at WarnLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func (log *Logger) Warn(msg string, fields ...Field) {
	if ce := log.check(WarnLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Error logs a message at ErrorLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func (log *Logger) Error(msg string, fields ...Field) {
	if ce := log.check(ErrorLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// DPanic logs a message at DPanicLevel. The message includes any fields
// passed at the log site, as well as any fields accumulated on the logger.
//
// If the logger is in development mode, it then panics (DPanic means
// "development panic"). This is useful for catching errors that are
// recoverable, but shouldn't ever happen.
func (log *Logger) DPanic(msg string, fields ...Field) {
	if ce := log.check(DPanicLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Panic logs a message at PanicLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
//
// The logger then panics, even if logging at PanicLevel is disabled.
func (log *Logger) Panic(msg string, fields ...Field) {
	if ce := log.check(PanicLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Fatal logs a message at FatalLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
//
// The logger then calls os.Exit(1), even if logging at FatalLevel is disabled.
func (log *Logger) Fatal(msg string, fields ...Field) {
	if ce := log.check(FatalLevel, msg); ce != nil {
		ce.Write(fields...)
	}
}

// Sync calls the underlying Core's Sync method, flushing any buffered log
// entries. Applications should take care to call Sync before exiting.
func (log *Logger) Sync() error {
	return log.core.Sync()
}

// Core returns the Logger's underlying zapcore.Core.
func (log *Logger) Core() zapcore.Core {
	return log.core
}

func (log *Logger) clone() *Logger {
	copy := *log
	return &copy
}

// 1. 创建 Entry 结构体并存储当前已确定的部分信息。
// 2. 调用 log.core.Check() 检查 lvl 级别的日志是否应该输出，若应该输出，就获取一个可用 CheckedEntry 的结构体 ce，并把 log.core 添加 ce.cores 中，并把 ent 赋值给 ce.Entry 。
// 3. 如果 ce != nil 则需要执行写操作，设置 willWrite 变量为 true ，否则直接返回 nil 。
// 4. 填充 ce.ErrorOutput、ce.Entry.Caller、ce.Entry.Stack 等信息。
// 5. 返回 ce 。
func (log *Logger) check(lvl zapcore.Level, msg string) *zapcore.CheckedEntry {

	// check must always be called directly by a method in the Logger interface (e.g., Check, Info, Fatal).
	const callerSkipOffset = 2

	// Create basic checked entry thru the core;
	// this will be non-nil if the log message will actually be written somewhere.
	//
	// 1. 创建 Entry 并存储当前已确定的部分信息，比如 logger name、timestamp、level、msg 字段。
	ent := zapcore.Entry{
		LoggerName: log.name,		// logger name
		Time:       time.Now(), 	// 时间
		Level:      lvl,			// 级别
		Message:    msg, 			// 内容
	}

	// 2. （重要）创建 CheckedEntry 结构体 ce 并把 log.core 添加 ce.cores 中，这些 ce.cores 会在 ce.Write() 中被逐个调用。
	ce := log.core.Check(ent, nil)

	// 3. 如果 ce 为 nil 则不会发生写行为
	willWrite := ce != nil

	// Set up any required terminal behavior.
	//
	// 判断 ent.Level 是否为特殊级别，主要是会导致进程退出的 PanicLevel，FatalLevel，DPanicLevel；
	// 在这几个级别下，进程已经发生了严重错误，需要特殊处理。
	switch ent.Level {
	case zapcore.PanicLevel:
		ce = ce.Should(ent, zapcore.WriteThenPanic)
	case zapcore.FatalLevel:
		ce = ce.Should(ent, zapcore.WriteThenFatal)
	case zapcore.DPanicLevel:
		if log.development {
			ce = ce.Should(ent, zapcore.WriteThenPanic)
		}
	}

	// Only do further annotation if we're going to write this message;
	// checked entries that exist only for terminal behavior don't benefit from annotation.
	//
	// 判断是否需要真正写日志，如果不需要写，这里就可以返回了，返回的 ce 是 nil 或者会使进程退出的一个 entry 。
	if !willWrite {
		return ce
	}


	// 4. 填充 ce 和 ce.Entry 中一些关键字段。

	// Thread the error output through to the CheckedEntry.
	ce.ErrorOutput = log.errorOutput

	// 判断是否需要打印文件名、行号，如果需要，调用 runtime.Caller(）获取并附加进entry里。
	if log.addCaller {
		// 保存调用者信息到 ce.Entry.Caller 中
		ce.Entry.Caller = zapcore.NewEntryCaller(runtime.Caller(log.callerSkip + callerSkipOffset))

		// 如果调用 runtime.Caller(）失败，则输出错误信息到 log.errorOutput 中，并实时的 sync 刷盘。
		if !ce.Entry.Caller.Defined {
			fmt.Fprintf(log.errorOutput, "%v Logger.check error: failed to get caller\n", time.Now().UTC())
			log.errorOutput.Sync()
		}
	}

	// 判断是否需要打印调用栈，如果需要，调用 runtime.CallersFrames(）获取并附加到 ce.Entry.Stack 里。
	if log.addStack.Enabled(ce.Entry.Level) {
		ce.Entry.Stack = Stack("").String
	}

	return ce
}
