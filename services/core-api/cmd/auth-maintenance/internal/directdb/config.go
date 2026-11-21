package directdb

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Config validates both the URL authority and the hosts pgx would connect to.
func Config(raw, applicationName string) (*pgx.ConnConfig, error) {
	invalid := errors.New("direct PostgreSQL URL required")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Fragment != "" {
		return nil, invalid
	}
	isPooled := func(host string) bool { return strings.Contains(strings.ToLower(host), "-pooler.") }
	if isPooled(u.Hostname()) {
		return nil, invalid
	}
	cfg, err := pgx.ParseConfig(raw)
	if err != nil || isPooled(cfg.Host) {
		return nil, invalid
	}
	for _, host := range cfg.Fallbacks {
		if isPooled(host.Host) {
			return nil, invalid
		}
	}
	cfg.RuntimeParams["application_name"] = applicationName
	cfg.ConnectTimeout = 10 * time.Second
	return cfg, nil
}
