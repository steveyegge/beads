package dolt

import (
	"regexp"
	"strings"
)

// urlUserinfoRe matches the "scheme://userinfo@host" span of a URL embedded
// anywhere in free-form text, such as the remote dolt echoes in a rejected
// push. The userinfo run stops at the first "@" or "/" so a trailing path is
// left intact.
var urlUserinfoRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^@\s'"/]*@[^/\s'"]*`)

// redactURLCredentials drops transport credentials from every URL embedded in
// s so CLI transfer errors can be shown in terminals and agent logs. HTTP(S)
// userinfo (for example x-access-token:<token>) is removed whole. SSH userinfo
// names the remote account and stays, minus any password. Text without a
// "userinfo@" URL is returned unchanged.
func redactURLCredentials(s string) string {
	return urlUserinfoRe.ReplaceAllStringFunc(s, func(span string) string {
		sep := strings.Index(span, "://")
		scheme, rest := span[:sep], span[sep+3:]
		at := strings.LastIndex(rest, "@")
		userinfo, host := rest[:at], rest[at+1:]
		if scheme == "ssh" || scheme == "git+ssh" {
			user, _, _ := strings.Cut(userinfo, ":")
			return scheme + "://" + user + "@" + host
		}
		return scheme + "://" + host
	})
}
