package dolt

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// traceSQL is true when BD_DOLT_TRACE_SQL=1. It enables coarse per-call
// stderr logging of SQL operations so we can count round trips and identify
// hot spots on slow remote-Dolt connections. Off by default.
var traceSQL = os.Getenv("BD_DOLT_TRACE_SQL") == "1"

var traceStart = time.Now()
var traceSeq atomic.Int64

// helperFrames are the internal helper functions we want to skip past when
// reporting the caller of a SQL op. We want to surface the DoltStore method
// (e.g. GetMetadata) or the cmd/bd caller, not the helper itself.
var helperFrames = []string{
	".withReadTx", ".withReadConn", ".withWriteTx", ".withRetryTx",
	".queryContext", ".execContext", ".queryRowContext",
	".sqlTraceStart", ".callerLabel",
}

// callerLabel walks up the stack and returns a short pkg.Func label for
// the first frame that isn't one of our internal SQL helpers.
func callerLabel(skip int) string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(skip+1, pcs)
	if n == 0 {
		return "?"
	}
	frames := runtime.CallersFrames(pcs[:n])
	for {
		fr, more := frames.Next()
		isHelper := false
		for _, h := range helperFrames {
			if strings.HasSuffix(fr.Function, h) || strings.Contains(fr.Function, h+".") {
				isHelper = true
				break
			}
		}
		if !isHelper {
			short := fr.Function
			if idx := strings.LastIndex(short, "/"); idx >= 0 {
				short = short[idx+1:]
			}
			return short
		}
		if !more {
			break
		}
	}
	return "?"
}

func sqlTraceStart(op string) func(extraFmt string, args ...any) {
	if !traceSQL {
		return func(string, ...any) {}
	}
	seq := traceSeq.Add(1)
	t0 := time.Now()
	rel := t0.Sub(traceStart)
	caller := callerLabel(2)
	stack := ""
	if os.Getenv("BD_DOLT_TRACE_STACK") == "1" {
		stack = "\n" + callerStack(2)
	}
	fmt.Fprintf(os.Stderr, "[sqltrace %4d] %8.1fms BEGIN %-36s via %s%s\n",
		seq, float64(rel.Microseconds())/1000.0, op, caller, stack)
	return func(extraFmt string, args ...any) {
		dt := time.Since(t0)
		extra := ""
		if extraFmt != "" {
			extra = " " + strings.TrimSpace(fmt.Sprintf(extraFmt, args...))
		}
		fmt.Fprintf(os.Stderr, "[sqltrace %4d] +%7.1fms END   %-36s%s\n",
			seq, float64(dt.Microseconds())/1000.0, op, extra)
	}
}

func callerStack(skip int) string {
	pcs := make([]uintptr, 24)
	n := runtime.Callers(skip+1, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var sb strings.Builder
	for {
		fr, more := frames.Next()
		short := fr.Function
		if idx := strings.LastIndex(short, "/"); idx >= 0 {
			short = short[idx+1:]
		}
		// skip runtime frames
		if strings.HasPrefix(short, "runtime.") {
			if !more {
				break
			}
			continue
		}
		sb.WriteString("    ")
		sb.WriteString(short)
		sb.WriteString("  (")
		sb.WriteString(fr.File)
		sb.WriteString(":")
		fmt.Fprintf(&sb, "%d", fr.Line)
		sb.WriteString(")\n")
		if !more {
			break
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
