package logger

import (
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"os"
	"path/filepath"
	"time"
)

const (
	clientColor = "\033[32m" // 绿色
	serverColor = "\033[34m" // 蓝色
	resetColor  = "\033[0m"  // 重置颜色
)

var (
	logDir   = "./logs"
	Logger   *zap.Logger
	colorMap = map[string]string{
		"client": clientColor,
		"server": serverColor,
	}
)

// 初始化日志模块（带文件切割和分级配置）
func InitLogger(env string) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		panic("创建日志目录失败: " + err.Error())
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder, // 级别颜色[4](@ref)
		EncodeTime:     customTimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 控制台使用带颜色的自定义编码器
	consoleEncoder := &sourceColorEncoder{
		Encoder: zapcore.NewConsoleEncoder(encoderConfig),
	}
	//colorFunc := func(fields []zapcore.Field) string {
	//	for _, f := range fields {
	//		if f.Key == "source" {
	//			return colorMap[f.String]
	//		}
	//	}
	//	return resetColor
	//}
	//consoleEncoder := &dynamicColorEncoder{
	//	Encoder:    zapcore.NewConsoleEncoder(encoderConfig),
	//	colorFunc:  colorFunc,
	//}

	// 文件使用JSON编码器（不带颜色）
	fileEncoder := zapcore.NewJSONEncoder(encoderConfig)

	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), zap.DebugLevel),
		zapcore.NewCore(fileEncoder, getFileWriter(), zap.DebugLevel),
	)

	options := []zap.Option{
		zap.AddCaller(),
		zap.AddCallerSkip(1),
		zap.AddStacktrace(zap.ErrorLevel),
	}

	if env == "development" {
		options = append(options, zap.Development())
	}

	Logger = zap.New(core, options...)
	zap.ReplaceGlobals(Logger)
}

// 自定义带颜色编码器
type sourceColorEncoder struct {
	zapcore.Encoder
}

func (e *sourceColorEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	buf, err := e.Encoder.EncodeEntry(ent, fields)
	if err != nil {
		return nil, err
	}

	color := resetColor
	for _, f := range fields {
		if f.Key == "source" {
			if c, ok := colorMap[f.String]; ok {
				color = c
			}
			break
		}
	}

	newBuf := buffer.NewPool().Get()
	newBuf.AppendString(color)
	newBuf.AppendString(buf.String())
	newBuf.AppendString(resetColor)
	return newBuf, nil
}

type dynamicColorEncoder struct {
	zapcore.Encoder
	colorFunc func(fields []zapcore.Field) string
}

func (e *dynamicColorEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	// 获取当前颜色
	currentColor := e.colorFunc(fields)
	reset := resetColor

	// 分阶段染色
	//buf, _ := e.Encoder.EncodeEntry(ent, fields)
	coloredBuf := buffer.NewPool().Get()

	// 消息体染色
	coloredBuf.AppendString(currentColor)
	coloredBuf.AppendString(ent.Message)
	coloredBuf.AppendString(reset)

	// 键值对染色
	for _, f := range fields {
		if f.Key == "source" {
			continue
		}
		coloredBuf.AppendString(" ")
		coloredBuf.AppendString(currentColor)
		coloredBuf.AppendString(f.Key)
		coloredBuf.AppendString("=")
		coloredBuf.AppendString(fmt.Sprintf("%v", f.Interface))
		coloredBuf.AppendString(reset)
	}

	return coloredBuf, nil
}

// 获取文件写入器（带日志切割）
func getFileWriter() zapcore.WriteSyncer {
	return zapcore.AddSync(&lumberjack.Logger{
		Filename:   filepath.Join(logDir, "app.log"),
		MaxSize:    100,  // MB
		MaxBackups: 30,   // 保留文件数
		MaxAge:     7,    // 保留天数
		Compress:   true, // 启用压缩
	})
}

// 自定义时间格式（保持与网页6兼容）
func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
}

// 封装日志方法（优化参数处理）
func Debug(msg string, fields ...zap.Field) {
	Logger.Debug(msg, fields...)
}

func Info(msg string, fields ...zap.Field) {
	Logger.Info(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	Logger.Warn(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	Logger.Error(msg, fields...)
}

func Fatal(msg string, fields ...zap.Field) {
	Logger.Fatal(msg, fields...)
}

// 客户端/服务端专用日志（优化颜色标记）
func ClientLog(msg string, fields ...zap.Field) {
	Logger.Debug(msg, append([]zap.Field{
		zap.String("source", "client"),
	}, fields...)...)
}

func ServerLog(msg string, fields ...zap.Field) {
	Logger.Debug(msg, append([]zap.Field{
		zap.String("source", "server"),
	}, fields...)...)
}
