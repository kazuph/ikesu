package guard

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// 確認（ask）のティア。L1 は手元だけで済む・判定できないだけのもの、L2 は宛先が
// 開発・検証と分かる外向きのもの、L3 は本番・取り返しがつかない・宛先が分からない・
// guard を変えるもの。決められない ask は L3 に倒す。
const (
	TierL1 = 1
	TierL2 = 2
	TierL3 = 3
)

// 宛先の名前。
const (
	DestProd     = "prod"
	DestDev      = "dev"
	DestEmulator = "emulator"
	DestGitHub   = "github"
	DestHTTP     = "http"
	DestUnknown  = "unknown"
)

// ask の判定へティア・宛先・ティアを決めた規則名を付ける。
func askAt(rule, reason string, tier int, dest string) Verdict {
	return Verdict{Decision: "ask", Rule: rule, Reason: reason, Tier: tier, Dest: dest, TierRule: rule}
}

// effectiveTier は ask のティア（付いていなければ L3）。ask 以外は 0。
func (v Verdict) effectiveTier() int {
	if v.Decision != "ask" {
		return 0
	}
	if v.Tier < TierL1 || v.Tier > TierL3 {
		return TierL3
	}
	return v.Tier
}

func (v Verdict) tierRuleName() string {
	if v.TierRule != "" {
		return v.TierRule
	}
	return v.Rule
}

// tierInfo は1行・1スクリプトの中でいちばん高いティア。
type tierInfo struct {
	tier int
	dest string
	rule string
}

// take は ask のティアが今の最大以上なら置き換える。同じティアでは、宛先が空の
// 判定が、宛先の付いた判定を隠さないようにする。
func (t *tierInfo) take(tier int, dest, rule string) {
	if tier < TierL1 {
		return
	}
	if tier > t.tier || tier == t.tier && (dest != "" || t.dest == "") {
		t.tier, t.dest, t.rule = tier, dest, rule
	}
}

func (t *tierInfo) takeVerdict(v Verdict) {
	if v.Decision == "ask" {
		t.take(v.effectiveTier(), v.Dest, v.tierRuleName())
	}
}

func (t *tierInfo) merge(o tierInfo) { t.take(o.tier, o.dest, o.rule) }

func (v Verdict) withTier(t tierInfo) Verdict {
	if v.Decision == "ask" && t.tier > 0 {
		v.Tier, v.Dest, v.TierRule = t.tier, t.dest, t.rule
	}
	return v
}

// 判定のエラーにティアを持たせる。付いていないエラーは L3。
type tieredError struct {
	err  error
	tier int
}

func (e tieredError) Error() string { return e.err.Error() }
func (e tieredError) Unwrap() error { return e.err }

func lowTierErrorf(format string, args ...any) error {
	return tieredError{fmt.Errorf(format, args...), TierL1}
}
func tierError(err error, tier int) error {
	if err == nil {
		return nil
	}
	return tieredError{err, tier}
}
func errTier(err error) int {
	var t tieredError
	if errors.As(err, &t) {
		return t.tier
	}
	return TierL3
}

// 宛先の決め方: 判定するコマンド自身の引数の値だけを見る。
var destinationFlags = map[string]bool{"--env": true, "-e": true, "--stage": true, "--target": true, "--profile": true}
var developmentNames = map[string]bool{"dev": true, "development": true, "stg": true, "staging": true, "preview": true}

func (i Input) unknownValue(v string) bool {
	return v == unresolvedPlaceholder || i.unknownWords[v]
}

func destinationTier(i Input) (int, string) {
	for _, a := range i.Args {
		if strings.Contains(strings.ToLower(a), "prod") {
			return TierL3, DestProd
		}
	}
	var values []string
	args := i.Args
	for n := 0; n < len(args); n++ {
		name, value, joined := strings.Cut(args[n], "=")
		if !destinationFlags[name] {
			continue
		}
		if !joined {
			if n+1 >= len(args) {
				return TierL3, DestUnknown
			}
			value = args[n+1]
			n++
		}
		if i.unknownValue(value) {
			return TierL3, DestUnknown
		}
		values = append(values, strings.ToLower(value))
	}
	if len(values) == 0 {
		return TierL3, DestUnknown
	}
	for _, v := range values {
		if !developmentNames[v] {
			return TierL3, DestUnknown
		}
	}
	return TierL2, DestDev
}

func destinationAsk(i Input, rule, reason string) Verdict {
	tier, dest := destinationTier(i)
	return askAt(rule, reason, tier, dest)
}

// loopbackURL は localhost・127.0.0.0/8・::1 だけを指す http(s) の URL かどうか。
func loopbackURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback() && !ip.Is4In6()
}

// awsAsk は aws の確認。エミュレーター（L1）と見なすのは、そのコマンドの引数に正確に
// --endpoint-url があり、値が全部ループバックの http(s) URL で、省略形（--endpoint など）が
// 無いときだけ。環境変数 AWS_ENDPOINT_URL は見ない（サービス別の AWS_ENDPOINT_URL_<サービス> が
// 優先され、AWS_IGNORE_CONFIGURED_ENDPOINT_URLS で無視もでき、行の中の unset・export も追えないため）。
func awsAsk(i Input) Verdict {
	const reason = "AWSコマンドには承認が必要です。"
	const flag = "--endpoint-url"
	var endpoints []string
	args := i.Args
	for n := 0; n < len(args); n++ {
		name, value, joined := strings.Cut(args[n], "=")
		if len(name) >= 3 && name != flag && strings.HasPrefix(flag, name) && strings.HasPrefix(name, "--e") {
			return askAt("aws", reason, TierL3, DestUnknown)
		}
		if name != flag {
			continue
		}
		if !joined {
			if n+1 >= len(args) {
				return askAt("aws", reason, TierL3, DestUnknown)
			}
			value = args[n+1]
			n++
		}
		endpoints = append(endpoints, value)
	}
	if len(endpoints) == 0 {
		return askAt("aws", reason, TierL3, DestUnknown)
	}
	for _, e := range endpoints {
		if i.unknownValue(e) || !loopbackURL(e) {
			return askAt("aws", reason, TierL3, DestUnknown)
		}
	}
	return askAt("aws", reason, TierL1, DestEmulator)
}
