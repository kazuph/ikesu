package guard

import (
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
)

// Only options with known destination semantics qualify for this exception.
func localHTTPDestinations(i Input) bool {
	cmd := filepath.Base(i.Command)
	if i.Unresolved || cmd == "wget" {
		return false
	}
	for key, value := range i.Env {
		if value != "" && has([]string{"http_proxy", "https_proxy", "all_proxy"}, strings.ToLower(key)) {
			return false
		}
	}
	urls := 0
	for n := 0; n < len(i.Args); n++ {
		a := i.Args[n]
		if cmd == "curl" && strings.HasPrefix(a, "--") {
			key, value, attached := strings.Cut(a, "=")
			switch key {
			case "--silent", "--show-error", "--fail", "--fail-with-body", "--globoff", "--disable":
				if attached {
					return false
				}
				continue
			case "--url", "--request", "--data", "--data-raw", "--data-binary", "--data-urlencode", "--json", "--form", "--form-string", "--header", "--output", "--max-time", "--connect-timeout":
				if !attached {
					n++
					if n >= len(i.Args) {
						return false
					}
					value = i.Args[n]
				}
				if key == "--url" {
					if !localHTTPURL(value) {
						return false
					}
					urls++
				}
				continue
			default:
				return false
			}
		}
		if cmd == "curl" && strings.HasPrefix(a, "-") {
			if len(a) == 1 {
				return false
			}
			for p := 1; p < len(a); p++ {
				switch a[p] {
				case 's', 'S', 'f', 'g', 'q':
				case 'X', 'd', 'F', 'H', 'o', 'm':
					if p == len(a)-1 {
						n++
						if n >= len(i.Args) {
							return false
						}
					}
					p = len(a)
				default:
					return false
				}
			}
			continue
		}
		if cmd == "http" || cmd == "https" {
			if has([]string{"POST", "PUT", "PATCH", "DELETE", "GET", "HEAD", "OPTIONS"}, a) && urls == 0 {
				continue
			}
			if has([]string{"--ignore-stdin", "--check-status", "--json", "--form"}, a) {
				continue
			}
			if strings.HasPrefix(a, "-") {
				return false
			}
			if urls > 0 && strings.ContainsAny(a, "=:") && !strings.Contains(a, "://") {
				continue
			}
		}
		if !localHTTPURL(a) {
			return false
		}
		urls++
	}
	return urls > 0
}

func localHTTPURL(raw string) bool {
	if strings.ContainsAny(raw, "$`{}\\\r\n\t ") {
		return false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip == netip.MustParseAddr("::1") || netip.MustParsePrefix("127.0.0.0/8").Contains(ip) || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip)
	}
	host = strings.TrimSuffix(host, ".")
	if host == "localhost" {
		return true
	}
	if !strings.HasSuffix(host, ".ts.net") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
