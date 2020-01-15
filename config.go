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
	"sort"
	"time"

	"github.com/blastbao/zap/zapcore"
)

// SamplingConfig sets a sampling strategy for the logger. Sampling caps the
// global CPU and I/O load that logging puts on your process while attempting
// to preserve a representative subset of your logs.
//
// Values configured here are per-second. See zapcore.NewSampler for details.
type SamplingConfig struct {
	Initial    int `json:"initial" yaml:"initial"`
	Thereafter int `json:"thereafter" yaml:"thereafter"`
}

// Config offers a declarative way to construct a logger. It doesn't do
// anything that can't be done with New, Options, and the various
// zapcore.WriteSyncer and zapcore.Core wrappers, but it's a simpler way to
// toggle common options.
//
// Note that Config intentionally supports only the most common options. More
// unusual logging setups (logging to network connections or message queues,
// splitting output between multiple files, etc.) are possible, but require
// direct use of the zapcore package. For sample code, see the package-level
// BasicConfiguration and AdvancedConfiguration examples.
//
// For an example showing runtime log level changes, see the documentation for
// AtomicLevel.


// Config 这个结构体每个字段都有 json 和 yaml 的标注，也就是说这些配置不仅仅可以在代码中赋值，
// 也可以从配置文件中直接反序列化得到。

type Config struct {

	// Level is the minimum enabled logging level. Note that this is a dynamic
	// level, so calling Config.Level.SetLevel will atomically change the log
	// level of all loggers descended from this config.
	//
	// Level：故名思意，Level 是用来配置日志级别的，即日志的最低输出级别。
	// 这里的 AtomicLevel 虽然是个结构体， 但是如果使用配置文件直接反序列化，
	// 可以支持配置成字符串“DEBUG”，“INFO”等，这一点很方便，实现也有一点小技巧，后面说。
	Level AtomicLevel `json:"level" yaml:"level"`

	// Development puts the logger in development mode, which changes the
	// behavior of DPanicLevel and takes stacktraces more liberally.
	//
	// 这个字段的含义是用来标记是否为开发者模式，在开发者模式下，日志输出的一些行为会和生产环境上不同。
	Development bool `json:"development" yaml:"development"`

	// DisableCaller stops annotating logs with the calling function's file
	// name and line number. By default, all logs are annotated.
	//
	// 用来标记是否开启行号和文件名显示功能。
	DisableCaller bool `json:"disableCaller" yaml:"disableCaller"`

	// DisableStacktrace completely disables automatic stacktrace capturing. By
	// default, stacktraces are captured for WarnLevel and above logs in
	// development and ErrorLevel and above in production.
	//
	// 标记是否开启调用栈追踪能力，即在打印异常日志时，是否打印调用栈。
	DisableStacktrace bool `json:"disableStacktrace" yaml:"disableStacktrace"`

	// Sampling sets a sampling policy. A nil SamplingConfig disables sampling.
	//
	// Sampling 实现了日志的流控功能，或者叫采样配置，主要有两个配置参数，Initial 和 Thereafter，
	// 实现的效果是在 1s 的时间单位内，如果某个日志级别下同样内容的日志输出数量超过了 Initial 的数量，
	// 那么超过之后，每隔 Thereafter 的数量，才会再输出一次。是一个对日志输出的保护功能。
	Sampling *SamplingConfig `json:"sampling" yaml:"sampling"`

	// Encoding sets the logger's encoding. Valid values are "json" and
	// "console", as well as any third-party encodings registered via RegisterEncoder.
	//
	// 用来指定日志的编码器，也就是用户在调用日志打印接口时，zap 内部使用什么样的编码器将日志信息编码为日志条目，
	// 日志的编码也是日志组件的一个重点。默认支持两种配置，json 和 console ，用户可以自行实现自己需要的编码器并注册进日志组件，
	// 实现自定义编码的能力。
	Encoding string `json:"encoding" yaml:"encoding"`

	// EncoderConfig sets options for the chosen encoder. See zapcore.EncoderConfig for details.
	//
	// EncoderConfig 是对于日志编码器的配置，支持的配置参数也很丰富。
	EncoderConfig zapcore.EncoderConfig `json:"encoderConfig" yaml:"encoderConfig"`


	// OutputPaths is a list of URLs or file paths to write logging output to.
	// See Open for details.
	//
	// 用来指定日志的输出路径，不过这个路径不仅仅支持文件路径和标准输出 stdout ，还支持其他的自定义协议，
	// 当然如果要使用自定义协议，也需要使用 RegisterSink 方法先注册一个该协议对应的工厂方法，该工厂方法实现了 Sink 接口。
	OutputPaths []string `json:"outputPaths" yaml:"outputPaths"`


	// ErrorOutputPaths is a list of URLs to write internal logger errors to.
	// The default is standard error.
	//
	// Note that this setting only affects internal errors; for sample code that
	// sends error-level logs to a different location from info- and debug-level
	// logs, see the package-level AdvancedConfiguration example.
	//
	// 与 OutputPaths 类似，不过指定的是系统内错误日志的输出地址，不是业务的错误（ERROR）日志。
	ErrorOutputPaths []string `json:"errorOutputPaths" yaml:"errorOutputPaths"`


	// InitialFields is a collection of fields to add to the root logger.
	//
	//
	// 加入一些初始的字段数据，比如项目名
	//
	// 理解这个参数需要结合 zap 的结构化日志输出的机制来理解，后面会详细解释，这里只要知道有这个配置时，日志输出内容中会包含这个 map 。
	InitialFields map[string]interface{} `json:"initialFields" yaml:"initialFields"`
}



// NewProductionEncoderConfig returns an opinionated EncoderConfig for production environments.
func NewProductionEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.EpochTimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

}

// NewProductionConfig is a reasonable production logging configuration.
// Logging is enabled at InfoLevel and above.
//
// It uses a JSON encoder, writes to standard error, and enables sampling.
// Stacktraces are automatically included on logs of ErrorLevel and above.
//
//
//
func NewProductionConfig() Config {
	return Config{
		Level: NewAtomicLevelAt(InfoLevel),
		Development: false,
		Sampling: &SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         "json",
		EncoderConfig:    NewProductionEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}
}


// NewDevelopmentEncoderConfig returns an opinionated EncoderConfig for development environments.
func NewDevelopmentEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		// Keys can be anything except the empty string.
		TimeKey:        "T",
		LevelKey:       "L",
		NameKey:        "N",
		CallerKey:      "C",
		MessageKey:     "M",
		StacktraceKey:  "S",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
}


// NewDevelopmentConfig is a reasonable development logging configuration.
// Logging is enabled at DebugLevel and above.
//
// It enables development mode (which makes DPanicLevel logs panic),
// uses a console encoder, writes to standard error, and disables sampling.
// Stacktraces are automatically included on logs of WarnLevel and above.
func NewDevelopmentConfig() Config {
	return Config{
		Level:            NewAtomicLevelAt(DebugLevel),
		Development:      true,
		Encoding:         "console",
		EncoderConfig:    NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}
}


// Build constructs a logger from the Config and Options.
func (cfg Config) Build(opts ...Option) (*Logger, error) {

	// 构造日志的编码器，cfg.buildEncoder() 实现中会用到 cfg.Encoding, cfg.EncoderConfig 这两个配置。
	enc, err := cfg.buildEncoder()
	if err != nil {
		return nil, err
	}

	// 构造日志的输出对象，在 cfg.openSinks 的实现中，使用配置的输出路径 cfg.OutputPaths ，生成了两个 WriteSyncer 接口，用作 `日志输出` 和 `内部错误输出` 。
	sink, errSink, err := cfg.openSinks()
	if err != nil {
		return nil, err
	}

	// 将 Core结构体 和 Option 作为参数调用 New 方法，这个方法会返回一个Logger。
	log := New(
		// 调用 NewCore 方法，传入上面构造出来的 encoder 和 sink 以及日志级别， 构造出日志的核心实现 `Core结构体`
		zapcore.NewCore(enc, sink, cfg.Level),
		// 调用 buildOptions 方法，将 Config 结构体转化成了 Option 接口数组
		cfg.buildOptions(errSink)...,
	)

	// 如果调用 Build 时还带有其他的 Option 参数，就调用 WithOptions 方法使这些Option生效
	if len(opts) > 0 {
		log = log.WithOptions(opts...)
	}

	// 返回 Logger
	return log, nil
}



//
func (cfg Config) buildOptions(errSink zapcore.WriteSyncer) []Option {


	opts := []Option{
		ErrorOutput(errSink),
	}

	// 开发者模式
	if cfg.Development {
		opts = append(opts, Development())
	}

	// 增加行号和文件名
	if !cfg.DisableCaller {
		opts = append(opts, AddCaller())
	}

	// 日志级别
	stackLevel := ErrorLevel
	if cfg.Development {
		stackLevel = WarnLevel
	}

	// 输出堆栈
	if !cfg.DisableStacktrace {
		opts = append(opts, AddStacktrace(stackLevel)) // AddStacktrace(level) 用来对指定的日志等级增加调用栈输出能力。
	}

	// 采样功能
	if cfg.Sampling != nil {
		opts = append(opts,
			WrapCore(
				//
				func(core zapcore.Core) zapcore.Core {
					return zapcore.NewSampler(core, time.Second, int(cfg.Sampling.Initial), int(cfg.Sampling.Thereafter))
				},
			),
		)
	}


	// 初始字段
	if len(cfg.InitialFields) > 0 {

		fs := make([]Field, 0, len(cfg.InitialFields))
		keys := make([]string, 0, len(cfg.InitialFields))

		// 提取 keys
		for k := range cfg.InitialFields {
			keys = append(keys, k)
		}

		// 排序 keys
		sort.Strings(keys)

		// 按序填充 key, value
		for _, k := range keys {
			fs = append(fs, Any(k, cfg.InitialFields[k]))
		}

		// 调用 Fields 创建 `为日志增加待打印的字段` 的 options，添加到 opts 中。
		opts = append(opts, Fields(fs...))
	}

	return opts
}

func (cfg Config) openSinks() (zapcore.WriteSyncer, zapcore.WriteSyncer, error) {
	sink, closeOut, err := Open(cfg.OutputPaths...)
	if err != nil {
		return nil, nil, err
	}
	errSink, _, err := Open(cfg.ErrorOutputPaths...)
	if err != nil {
		closeOut()
		return nil, nil, err
	}
	return sink, errSink, nil
}

func (cfg Config) buildEncoder() (zapcore.Encoder, error) {
	return newEncoder(cfg.Encoding, cfg.EncoderConfig)
}
