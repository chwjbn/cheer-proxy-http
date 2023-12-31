package cheerapp

import (
	"context"
	"fmt"
	"github.com/chwjbn/cheer-proxy-http/cheerlib"
	"strings"
)

func LogInfo(logFmt string, args ...interface{}) {
	LogInfoWithContext(context.Background(), logFmt, args...)
}

func LogWarn(logFmt string, args ...interface{}) {
	LogWarnWithContext(context.Background(), logFmt, args...)
}

func LogError(logFmt string, args ...interface{}) {
	LogErrorWithContext(context.Background(), logFmt, args...)
}

func LogInfoWithContext(ctx context.Context, logFmt string, args ...interface{}) {
	writeLogWithContext(ctx, "INFO", logFmt, args...)
}

func LogWarnWithContext(ctx context.Context, logFmt string, args ...interface{}) {
	writeLogWithContext(ctx, "WARN", logFmt, args...)
}

func LogErrorWithContext(ctx context.Context, logFmt string, args ...interface{}) {
	writeLogWithContext(ctx, "ERROR", logFmt, args...)
}

func writeLogWithContext(ctx context.Context, logLevel string, logFmt string, args ...interface{}) {

	xLogContent := fmt.Sprintf("[%s][%s]", cheerlib.TimeGetNow(), logLevel) + logFmt

	if len(args) > 0 {
		xLogContent = fmt.Sprintf("[%s][%s]", cheerlib.TimeGetNow(), logLevel) + fmt.Sprintf(logFmt, args...)
	}

	writeLogContent(ctx, xLogContent)
}

func writeLogContent(ctx context.Context, logContent string) {
	if gSkyapmLogger == nil {
		return
	}

	xErrorLevel := "INFO"
	if strings.Contains(logContent, "[DEBUG]") {
		xErrorLevel = "DEBUG"
	}

	if strings.Contains(logContent, "[INFO]") {
		xErrorLevel = "INFO"
	}

	if strings.Contains(logContent, "[WARN]") {
		xErrorLevel = "WARN"
	}

	if strings.Contains(logContent, "[ERROR]") {
		xErrorLevel = "ERROR"
	}

	gSkyapmLogger.WriteLogWithContext(ctx, xErrorLevel, logContent)
}
