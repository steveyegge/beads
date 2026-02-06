package rpc

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// handleAdminGC runs dolt gc on the server-side Dolt repository (bd-ma0s.5).
func (s *Server) handleAdminGC(req *Request) Response {
	var args AdminGCArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid admin_gc args: %v", err),
		}
	}

	// Derive dolt path from the database path.
	// dbPath is typically .beads/beads.db; dolt dir is .beads/dolt
	doltPath := filepath.Join(filepath.Dir(s.dbPath), "dolt")
	if _, err := os.Stat(doltPath); os.IsNotExist(err) {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("dolt directory not found at %s", doltPath),
		}
	}

	// Check that dolt binary is available
	if _, err := exec.LookPath("dolt"); err != nil {
		return Response{
			Success: false,
			Error:   "dolt command not found in PATH",
		}
	}

	start := time.Now()

	// Get size before GC
	bytesBefore, err := adminGetDirSize(doltPath)
	if err != nil {
		bytesBefore = 0
	}

	if args.DryRun {
		elapsed := time.Since(start)
		result := AdminGCResult{
			DoltPath:    doltPath,
			BytesBefore: bytesBefore,
			BytesAfter:  bytesBefore,
			SpaceFreed:  0,
			DryRun:      true,
			ElapsedMs:   elapsed.Milliseconds(),
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Run dolt gc with a timeout context
	ctx, cancel := s.reqCtx(req)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dolt", "gc") // #nosec G204 -- fixed command, no user input
	cmd.Dir = doltPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("dolt gc failed: %v (output: %s)", err, string(output)),
		}
	}

	// Get size after GC
	bytesAfter, err := adminGetDirSize(doltPath)
	if err != nil {
		bytesAfter = 0
	}

	elapsed := time.Since(start)
	spaceFreed := bytesBefore - bytesAfter
	if spaceFreed < 0 {
		spaceFreed = 0
	}

	result := AdminGCResult{
		DoltPath:    doltPath,
		BytesBefore: bytesBefore,
		BytesAfter:  bytesAfter,
		SpaceFreed:  spaceFreed,
		ElapsedMs:   elapsed.Milliseconds(),
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// adminGetDirSize calculates the total size of a directory.
func adminGetDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}
