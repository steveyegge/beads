package api

import "time"

func timePtr(t time.Time) *time.Time {
	return &t
}
