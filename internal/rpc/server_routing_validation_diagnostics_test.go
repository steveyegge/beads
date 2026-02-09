package rpc

import "testing"

func TestSkipAutoImport(t *testing.T) {
	// Operations that should skip auto-import
	shouldSkip := []string{
		OpPing, OpHealth, OpMetrics, OpExport,
		OpShutdown,
		OpGetMutations, OpGetMoleculeProgress, OpGetWorkerStatus, OpGetConfig, OpMolStale, OpCompactStats,
		OpGateCreate, OpGateList, OpGateShow, OpGateClose, OpGateWait,
	}
	for _, op := range shouldSkip {
		if !skipAutoImport(op) {
			t.Errorf("skipAutoImport(%q) = false, want true", op)
		}
	}

	// Operations that should NOT skip auto-import (data operations need fresh state)
	shouldNotSkip := []string{
		OpCreate, OpUpdate, OpClose, OpDelete,
		OpList, OpCount, OpShow, OpReady, OpBlocked, OpStale, OpStats,
		OpResolveID, OpBatch,
		OpDepAdd, OpDepRemove,
		OpLabelAdd, OpLabelRemove,
		OpCommentList, OpCommentAdd,
		OpCompact, OpEpicStatus,
	}
	for _, op := range shouldNotSkip {
		if skipAutoImport(op) {
			t.Errorf("skipAutoImport(%q) = true, want false", op)
		}
	}
}
