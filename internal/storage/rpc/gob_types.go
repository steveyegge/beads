package rpc

import (
	"encoding/gob"
	"encoding/json"
	"time"
)

func init() {
	// UpdateIssueArgs.Updates is map[string]interface{}. gob requires explicit
	// registration of each concrete type stored in interface{} values at program
	// init, or encoding panics at runtime with "type not registered for interface".
	gob.Register(time.Time{})
	gob.Register(json.RawMessage(nil))
	gob.Register([]string(nil))
	gob.Register(map[string]interface{}(nil))
}
