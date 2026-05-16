package dsn

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// BuildFromFields composes a stripped (password-free) DSN from individual
// connection fields. The result is suitable for persistence in metadata.json
// and round-trips cleanly through pgconn.ParseConfig + ConfigToConnString.
//
// sslmode defaults to "disable" when empty.
func BuildFromFields(host string, port int, user, db, sslmode string) string {
	if sslmode == "" {
		sslmode = "disable"
	}
	u := url.URL{Scheme: "postgres"}
	if user != "" {
		u.User = url.User(user)
	}
	if port != 0 {
		u.Host = net.JoinHostPort(host, strconv.Itoa(port))
	} else {
		u.Host = host
	}
	if db != "" {
		u.Path = "/" + db
	}
	u.RawQuery = fmt.Sprintf("sslmode=%s", url.QueryEscape(sslmode))
	return u.String()
}
