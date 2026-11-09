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
	if i.Unresolved || cmd == "wget" || !noHTTPDefaultConfig(i, cmd) {
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
			case "--url", "--request", "--data", "--data-ascii", "--data-raw", "--data-binary", "--data-urlencode", "--json", "--form", "--form-string", "--header", "--output", "--max-time", "--connect-timeout", "--write-out", "--user-agent", "--referer", "--cookie", "--user", "--range", "--continue-at", "--speed-limit", "--speed-time", "--limit-rate", "--rate", "--retry", "--retry-delay", "--retry-max-time", "--max-filesize", "--expect100-timeout", "--happy-eyeballs-timeout-ms", "--keepalive-time", "--url-query",
				"--aws-sigv4", "--cacert", "--capath", "--cert", "--cert-type", "--ciphers", "--crlfile", "--curves", "--delegation", "--egd-file", "--engine", "--etag-compare", "--ftp-account", "--ftp-alternative-to-user", "--ftp-method", "--ftp-port", "--ftp-ssl-ccc-mode", "--haproxy-clientip", "--hostpubmd5", "--hostpubsha256", "--interface", "--ipfs-gateway", "--key", "--key-type", "--krb", "--local-port", "--login-options", "--mail-auth", "--mail-from", "--mail-rcpt", "--max-redirs", "--netrc-file", "--noproxy", "--oauth2-bearer", "--parallel-max", "--pass", "--pinnedpubkey", "--proto", "--proto-redir", "--pubkey", "--quote", "--random-file", "--request-target", "--sasl-authzid", "--service-name", "--socks5-gssapi-service", "--telnet-option", "--tftp-blksize", "--time-cond", "--tls-max", "--tls13-ciphers", "--tlsauthtype", "--tlspassword", "--tlsuser", "--upload-file", "--variable":
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
				if key == "--write-out" && unsafeCurlWriteOut(value) {
					return false
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
				case 'X', 'd', 'F', 'H', 'o', 'm', 'w', 'A', 'e', 'b', 'u', 'r', 'C', 'Y', 'y', 'E', 'P', 'Q', 't', 'z', 'T':
					value := a[p+1:]
					if p == len(a)-1 {
						n++
						if n >= len(i.Args) {
							return false
						}
						value = i.Args[n]
					}
					if a[p] == 'w' && unsafeCurlWriteOut(value) {
						return false
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

func unsafeCurlWriteOut(value string) bool {
	return strings.HasPrefix(value, "@") || strings.Contains(value, "%output{")
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

// Curl option values are operands even when their contents look like flags.
func curlHTTPMethod(args []string) string {
	method := ""
	body := false
	for n := 0; n < len(args); n++ {
		a := args[n]
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "--") {
			key, value, attached := strings.Cut(a, "=")
			key, known := curlCanonicalLongOption(key)
			if !known {
				continue
			}
			if !attached {
				n++
				if n >= len(args) {
					break
				}
				value = args[n]
			}
			if key == "--request" {
				method = strings.ToUpper(value)
			}
			if curlBodyOption(key) {
				body = true
			}
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		for p := 1; p < len(a); p++ {
			flag := a[p]
			if !strings.ContainsRune("AbCcDdEeEFHhKm oP QrtTuUXwYyxz", rune(flag)) || flag == ' ' {
				continue
			}
			value := a[p+1:]
			if value == "" {
				n++
				if n >= len(args) {
					break
				}
				value = args[n]
			}
			if flag == 'X' {
				method = strings.ToUpper(value)
			}
			if flag == 'd' || flag == 'F' || flag == 'T' {
				body = true
			}
			break
		}
	}
	if method != "" {
		return method
	}
	if body {
		return "POST"
	}
	return "GET"
}

func curlLongOptionValue(name string) bool {
	return has(strings.Fields(curlLongOptionNames), name)
}

var curlBodyLongOptions = []string{"--data", "--data-ascii", "--data-binary", "--data-raw", "--data-urlencode", "--json", "--form", "--form-string", "--upload-file"}

// Long options that take no value but are a prefix of one that does.
var curlValuelessLongOptions = []string{"--head"}

func curlBodyOption(key string) bool {
	return has(curlBodyLongOptions, key)
}

// curlCanonicalLongOption maps an abbreviated long option to the name curl
// resolves it to. When several known names share the prefix, a body option wins
// (and then --request), so the result errs towards asking. The bool reports
// whether the option takes a value.
func curlCanonicalLongOption(key string) (string, bool) {
	if curlLongOptionValue(key) {
		return key, true
	}
	if has(curlValuelessLongOptions, key) {
		return key, false
	}
	if len(key) <= 2 {
		return key, false
	}
	var found []string
	for _, name := range strings.Fields(curlLongOptionNames) {
		if strings.HasPrefix(name, key) {
			found = append(found, name)
		}
	}
	for _, name := range found {
		if has(curlBodyLongOptions, name) {
			return name, true
		}
	}
	for _, name := range found {
		if name == "--request" {
			return name, true
		}
	}
	if len(found) > 0 {
		return found[0], true
	}
	return key, false
}

const curlLongOptionNames = `--abstract-unix-socket --alt-svc --aws-sigv4 --cacert --capath --cert-type --cert --ciphers --config --connect-timeout --connect-to --continue-at --cookie-jar --cookie --create-file-mode --crlfile --curves --data-ascii --data-binary --data-raw --data-urlencode --data --delegation --dns-interface --dns-ipv4-addr --dns-ipv6-addr --dns-servers --doh-url --dump-header --egd-file --engine --etag-compare --etag-save --expect100-timeout --form-string --form --ftp-account --ftp-alternative-to-user --ftp-method --ftp-port --ftp-ssl-ccc-mode --happy-eyeballs-timeout-ms --haproxy-clientip --header --help --hostpubmd5 --hostpubsha256 --hsts --interface --ipfs-gateway --json --keepalive-time --key-type --key --krb --libcurl --limit-rate --local-port --login-options --mail-auth --mail-from --mail-rcpt --max-filesize --max-redirs --max-time --netrc-file --noproxy --oauth2-bearer --output-dir --output --parallel-max --pass --pinnedpubkey --proto-default --proto-redir --proto --proxy-cacert --proxy-capath --proxy-cert-type --proxy-cert --proxy-ciphers --proxy-crlfile --proxy-header --proxy-key-type --proxy-key --proxy-pass --proxy-pinnedpubkey --proxy-service-name --proxy-tls13-ciphers --proxy-tlsauthtype --proxy-tlspassword --proxy-tlsuser --proxy-user --proxy --proxy1.0 --pubkey --quote --random-file --range --rate --referer --request-target --request --resolve --retry-delay --retry-max-time --retry --sasl-authzid --service-name --socks4 --socks4a --socks5-gssapi-service --socks5-hostname --socks5 --speed-limit --speed-time --stderr --telnet-option --tftp-blksize --time-cond --tls-max --tls13-ciphers --tlsauthtype --tlspassword --tlsuser --trace-ascii --trace-config --trace --unix-socket --upload-file --url-query --url --user-agent --user --variable --write-out`
