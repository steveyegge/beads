package rpc

import "net/rpc"

// DaemonStats aggregates runtime metrics from a running daemonServer.
// Extended by be-732qlr with iterator-session fields (be-60kmhm §7).
type DaemonStats struct {
	// Iterator session metrics
	IterSessionsActive     int64 // gauge: current open sessions
	IterSessionStartsTotal int64 // counter: total successful IterXxxStart calls
	IterSessionReapedTotal int64 // counter: sessions reaped by idle reaper
	IterRowsStreamedTotal  int64 // counter: rows delivered via IterXxxNext
	IterSessionCapacity    int   // config value: daemon_iter_max
}

// GetDaemonStatsArgs has no fields — stats take no arguments.
type GetDaemonStatsArgs struct{}

// GetDaemonStatsReply carries the stats and a possible error.
type GetDaemonStatsReply struct {
	Stats    DaemonStats
	RPCError *RPCError
}

// GetDaemonStats serves the stats RPC call from the daemon server.
func (s *daemonServer) GetDaemonStats(_ *GetDaemonStatsArgs, reply *GetDaemonStatsReply) error {
	reply.Stats = DaemonStats{
		IterSessionsActive:     s.iterMgr.sessionsActive.Load(),
		IterSessionStartsTotal: s.iterMgr.sessionStartsTotal.Load(),
		IterSessionReapedTotal: s.iterMgr.sessionReapedTotal.Load(),
		IterRowsStreamedTotal:  s.iterMgr.rowsStreamedTotal.Load(),
		IterSessionCapacity:    s.iterMgr.maxCap,
	}
	return nil
}

// GetDaemonStats fetches runtime statistics from the daemon over RPC.
func (c *daemonClient) GetDaemonStats() (DaemonStats, error) {
	return GetStats(c.client)
}

// GetStats fetches DaemonStats from an already-dialed *rpc.Client.
// This is the entry point for callers that hold the raw RPC connection
// (e.g. cmd/bd/daemon_stats.go) without a daemonClient handle.
func GetStats(client *rpc.Client) (DaemonStats, error) {
	args := &GetDaemonStatsArgs{}
	var reply GetDaemonStatsReply
	if err := client.Call("daemonServer.GetDaemonStats", args, &reply); err != nil {
		return DaemonStats{}, err
	}
	return reply.Stats, decodeRPCError(reply.RPCError)
}
