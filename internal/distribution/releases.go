// Package distribution installs explicitly requested, immutable plugin releases.
package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Product struct {
	Name, Repository, TagPrefix, Asset, Root, Manifest, PluginID string
	Minimum                                                      string
}

var TerminalBrowser = Product{
	Name: "terminal-browser", Repository: "kazuph/herdr", TagPrefix: "kazuph-tb-v",
	Asset: "terminal-browser-darwin-arm64.tar.gz", Root: "terminal-browser",
	Manifest: "ikesu-plugin/ikesu-plugin.toml", Minimum: "0.7.3",
}

var MacDialog = Product{
	Name: "mac-dialog-decisions", Repository: "kazuph/ikesu", TagPrefix: "ikesu-mac-dialog-v",
	Asset: "ikesu-mac-dialog-darwin-arm64.tar.gz", Root: "mac-dialog-decisions",
	Manifest: "ikesu-plugin.toml", PluginID: "kazuph.mac-dialog-decisions",
}

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type Release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

type Fetcher interface {
	Fetch(context.Context, string, string) error
}

type Curl struct{}

func (Curl) Fetch(ctx context.Context, url, destination string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("release download requires HTTPS")
	}
	cmd := exec.CommandContext(ctx, "curl", "--fail", "--silent", "--show-error", "--location", "--proto", "=https", "--proto-redir", "=https", "--output", destination, "--", url)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("release download: %w: %s", err, output)
	}
	return nil
}

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func version(value string) ([3]uint64, error) {
	var v [3]uint64
	match := versionPattern.FindStringSubmatch(value)
	if match == nil {
		return v, fmt.Errorf("expected exact MAJOR.MINOR.PATCH version, got %q", value)
	}
	for i := range v {
		n, err := strconv.ParseUint(match[i+1], 10, 64)
		if err != nil {
			return v, err
		}
		v[i] = n
	}
	return v, nil
}

func newer(a, b string) bool {
	x, _ := version(a)
	y, _ := version(b)
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func (p Product) Select(ctx context.Context, fetch Fetcher, apiRoot, requested string) (Release, error) {
	if requested != "" {
		if _, err := version(requested); err != nil {
			return Release{}, err
		}
	}
	if requested != "" && p.Minimum != "" && newer(p.Minimum, requested) {
		return Release{}, fmt.Errorf("%s requires version %s or newer", p.Name, p.Minimum)
	}
	work, err := os.MkdirTemp("", "ikesu-release-*")
	if err != nil {
		return Release{}, err
	}
	defer os.RemoveAll(work)
	var best Release
	for page := 1; ; page++ {
		file := filepath.Join(work, "releases.json")
		url := fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", apiRoot, p.Repository, page)
		if err := fetch.Fetch(ctx, url, file); err != nil {
			return Release{}, err
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return Release{}, err
		}
		var releases []Release
		if err := json.Unmarshal(data, &releases); err != nil {
			return Release{}, err
		}
		for _, r := range releases {
			if r.Draft || r.Prerelease || !strings.HasPrefix(r.Tag, p.TagPrefix) {
				continue
			}
			v := strings.TrimPrefix(r.Tag, p.TagPrefix)
			if _, err := version(v); err != nil {
				continue
			}
			if requested != "" && requested != v || p.Minimum != "" && newer(p.Minimum, v) {
				continue
			}
			if _, _, err := p.URLs(r); err != nil {
				continue
			}
			if best.Tag == "" || newer(v, strings.TrimPrefix(best.Tag, p.TagPrefix)) {
				best = r
			}
		}
		if len(releases) < 100 {
			break
		}
	}
	if best.Tag == "" {
		return best, fmt.Errorf("no complete stable %s release available", p.Name)
	}
	return best, nil
}

func (p Product) URLs(r Release) (string, string, error) {
	prefix := "https://github.com/" + p.Repository + "/releases/download/" + r.Tag + "/"
	archive, sums := "", ""
	for _, a := range r.Assets {
		if a.Name != p.Asset && a.Name != "SHA256SUMS" {
			continue
		}
		if a.URL != prefix+a.Name {
			return "", "", fmt.Errorf("unexpected release asset URL %q", a.URL)
		}
		if a.Name == p.Asset {
			if archive != "" {
				return "", "", fmt.Errorf("duplicate archive")
			}
			archive = a.URL
		} else {
			if sums != "" {
				return "", "", fmt.Errorf("duplicate checksums")
			}
			sums = a.URL
		}
	}
	if archive == "" || sums == "" {
		return "", "", fmt.Errorf("incomplete release")
	}
	return archive, sums, nil
}

func Checksum(sums []byte, name string) (string, error) {
	found := ""
	seen := map[string]bool{}
	for _, line := range strings.Split(string(sums), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid SHA256SUMS line")
		}
		file := strings.TrimPrefix(parts[1], "*")
		if seen[file] {
			return "", fmt.Errorf("duplicate SHA256SUMS entry %q", file)
		}
		seen[file] = true
		decoded, err := hex.DecodeString(parts[0])
		if err != nil || len(decoded) != sha256.Size {
			return "", fmt.Errorf("invalid SHA256 checksum")
		}
		if file == name {
			found = strings.ToLower(parts[0])
		}
	}
	if found == "" {
		return "", fmt.Errorf("missing SHA256SUMS entry %q", name)
	}
	return found, nil
}
