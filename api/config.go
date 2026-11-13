package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type authConfig struct {
	secure  bool
	ttl     time.Duration
	origins map[string]bool
	proxies []*net.IPNet
}

func canonicalOrigin(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", fmt.Errorf("invalid origin")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("invalid origin scheme")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.ContainsAny(host, " ,\t\r\n") {
		return "", fmt.Errorf("invalid host")
	}
	port := u.Port()
	if port == "" {
		if scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid port")
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(n)), nil
}
func loadAuthConfig() (authConfig, error) {
	c := authConfig{ttl: 12 * time.Hour, origins: map[string]bool{}}
	var e error
	if s := os.Getenv("AUTH_COOKIE_SECURE"); s != "" {
		c.secure, e = strconv.ParseBool(s)
		if e != nil {
			return c, e
		}
	}
	if s := os.Getenv("AUTH_SESSION_TTL"); s != "" {
		c.ttl, e = time.ParseDuration(s)
		if e != nil || c.ttl <= 0 {
			return c, fmt.Errorf("invalid session TTL")
		}
	}
	for _, raw := range strings.Split(os.Getenv("AUTH_ALLOWED_ORIGINS"), ",") {
		o, e := canonicalOrigin(strings.TrimSpace(raw))
		if e != nil {
			return c, fmt.Errorf("AUTH_ALLOWED_ORIGINS: %w", e)
		}
		c.origins[o] = true
	}
	for _, s := range strings.Split(os.Getenv("AUTH_TRUSTED_PROXIES"), ",") {
		if strings.TrimSpace(s) == "" {
			continue
		}
		_, n, e := net.ParseCIDR(strings.TrimSpace(s))
		if e != nil {
			return c, e
		}
		c.proxies = append(c.proxies, n)
	}
	return c, nil
}
func (c authConfig) trusted(ip net.IP) bool {
	for _, n := range c.proxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
