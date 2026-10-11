package guard

import "strings"

func localWrangler(i Input) bool {
	seen := false
	for _, t := range append([]string{i.Command}, i.Args...) {
		t = t[strings.LastIndex(t, "/")+1:]
		if t == "wrangler" || strings.HasPrefix(t, "wrangler@") {
			seen = true
			continue
		}
		if seen && t == "--local" {
			return true
		}
	}
	return false
}
func d1(i Input) bool {
	seen, next := false, false
	for _, t := range append([]string{i.Command}, i.Args...) {
		t = t[strings.LastIndex(t, "/")+1:]
		if t == "wrangler" || strings.HasPrefix(t, "wrangler@") {
			seen = true
			next = false
			continue
		}
		if !seen {
			continue
		}
		if t == "d1" {
			next = true
			continue
		}
		if next && t == "execute" {
			return true
		}
		if next {
			next = false
		}
	}
	return false
}
func exactD1Help(i Input) bool {
	return i.Command == "wrangler" && strings.Join(i.Args, " ") == "d1 execute --help" || i.Command == "npx" && strings.Join(i.Args, " ") == "wrangler d1 execute --help"
}
func fileSQL(a []string) bool {
	for _, v := range a {
		if v == "--file" || strings.HasPrefix(v, "--file=") {
			return true
		}
	}
	return false
}
func sqlArgument(a []string) string {
	for n, v := range a {
		if has([]string{"--command", "-c", "--execute", "-e"}, v) {
			return arg(a, n+1)
		}
		if strings.HasPrefix(v, "--command=") || strings.HasPrefix(v, "--execute=") {
			return v[strings.Index(v, "=")+1:]
		}
		f := strings.Fields(strings.ToLower(v))
		if len(f) > 0 && has(strings.Fields("select insert update delete alter create drop truncate show describe desc explain pragma"), f[0]) {
			return v
		}
	}
	return ""
}
func readonlySQL(s string) bool {
	if s == "" {
		return false
	}
	lines := strings.Split(s, "\n")
	for n, l := range lines {
		lines[n], _, _ = strings.Cut(l, "--")
	}
	s = strings.Join(lines, " ")
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "/*") {
		_, tail, ok := strings.Cut(s, "*/")
		if !ok {
			return false
		}
		s = strings.TrimSpace(tail)
	}
	s = strings.TrimRight(s, "; \t\n\r")
	if strings.Contains(s, ";") {
		return false
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return false
	}
	switch strings.ToUpper(f[0]) {
	case "SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN":
		return true
	case "PRAGMA":
		if strings.Contains(s, "=") {
			return false
		}
		p := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(s, f[0])))
		end := strings.IndexFunc(p, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r == '_') })
		if end >= 0 {
			p = p[:end]
		}
		return has(strings.Fields("table_info table_list table_xinfo database_list index_list index_info index_xinfo foreign_key_list foreign_key_check quick_check integrity_check compile_options page_count page_size freelist_count data_version schema_version collation_list function_list module_list pragma_list"), p)
	}
	return false
}
func writeMethod(s string) bool { return has([]string{"POST", "PUT", "PATCH", "DELETE"}, s) }
func httpMethod(a []string, cmd string) string {
	if has([]string{"http", "https"}, cmd) && writeMethod(arg(a, 0)) {
		return a[0]
	}
	method := ""
	body := false
	for n, v := range a {
		switch {
		case has([]string{"-X", "--request", "--method"}, v):
			method = strings.ToUpper(arg(a, n+1))
		case strings.HasPrefix(v, "-X") && len(v) > 2:
			method = strings.ToUpper(v[2:])
		case strings.HasPrefix(v, "--request=") || strings.HasPrefix(v, "--method="):
			method = strings.ToUpper(v[strings.Index(v, "=")+1:])
		case has(strings.Fields("-d --data --data-raw --data-binary --data-urlencode --json -F --form"), v) || strings.HasPrefix(v, "-d") || strings.HasPrefix(v, "--data=") || strings.HasPrefix(v, "--data-raw=") || strings.HasPrefix(v, "--data-binary=") || strings.HasPrefix(v, "--data-urlencode=") || strings.HasPrefix(v, "--json=") || strings.HasPrefix(v, "--form="):
			body = true
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
