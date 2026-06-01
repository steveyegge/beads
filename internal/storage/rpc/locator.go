package rpc

import (
	"github.com/steveyegge/beads/internal/storage"
)

// Compile-time assertions.
var _ storage.Storage = (*daemonClient)(nil)
var _ storage.StoreLocator = (*daemonClient)(nil)

// LocatorPathArgs / LocatorPathReply are the RPC types for the StoreLocator.Path call.
type LocatorPathArgs struct{}

type LocatorPathReply struct {
	Value    string
	RPCError *RPCError
}

// LocatorCLIDirArgs / LocatorCLIDirReply are the RPC types for StoreLocator.CLIDir.
type LocatorCLIDirArgs struct{}

type LocatorCLIDirReply struct {
	Value    string
	RPCError *RPCError
}

// LocatorPath serves the StoreLocator.Path RPC call from the daemon server.
func (s *daemonServer) LocatorPath(_ *LocatorPathArgs, reply *LocatorPathReply) error {
	sl, ok := s.store.(storage.StoreLocator)
	if !ok {
		reply.RPCError = &RPCError{Kind: "", Msg: "store does not implement StoreLocator"}
		return nil
	}
	reply.Value = sl.Path()
	return nil
}

// LocatorCLIDir serves the StoreLocator.CLIDir RPC call from the daemon server.
func (s *daemonServer) LocatorCLIDir(_ *LocatorCLIDirArgs, reply *LocatorCLIDirReply) error {
	sl, ok := s.store.(storage.StoreLocator)
	if !ok {
		reply.RPCError = &RPCError{Kind: "", Msg: "store does not implement StoreLocator"}
		return nil
	}
	reply.Value = sl.CLIDir()
	return nil
}

// Path implements storage.StoreLocator for the daemon client.
func (c *daemonClient) Path() string {
	args := &LocatorPathArgs{}
	var reply LocatorPathReply
	if err := c.client.Call("daemonServer.LocatorPath", args, &reply); err != nil {
		return ""
	}
	return reply.Value
}

// CLIDir implements storage.StoreLocator for the daemon client.
func (c *daemonClient) CLIDir() string {
	args := &LocatorCLIDirArgs{}
	var reply LocatorCLIDirReply
	if err := c.client.Call("daemonServer.LocatorCLIDir", args, &reply); err != nil {
		return ""
	}
	return reply.Value
}
