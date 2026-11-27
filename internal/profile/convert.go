package profile

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/metacubex/mihomo/common/convert"
	"go.yaml.in/yaml/v3"

	"gopurr/internal/localize"
	"gopurr/internal/patch"
)

// ParseFailure reports a single malformed line or entry while allowing other
// valid entries in the same paste/payload to succeed.
type ParseFailure struct {
	Line   int    `json:"line"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// ParseResult classifies pasted/imported proxy inputs and returns parsed nodes
// alongside per-line failures for partial tolerance.
type ParseResult struct {
	Kind          string            `json:"kind"` // "url", "nodes", "clash_yaml", "empty", "unknown"
	URL           string            `json:"url,omitempty"`
	YAML          string            `json:"yaml,omitempty"`
	Decoded       bool              `json:"decoded,omitempty"`
	AmbiguousHTTP bool              `json:"ambiguousHttp,omitempty"`
	Nodes         []patch.ProxyNode `json:"nodes"`
	Failures      []ParseFailure    `json:"failures"`
}

// ParseProxyInputs auto-detects subscription URLs, single/multi-line share URIs,
// Base64-encoded payloads (re-detecting Clash YAML vs node URIs), and Clash YAML.
func ParseProxyInputs(text string) ParseResult {
	return ParseProxyInputsWithMode(text, "auto")
}

// ParseProxyInputsWithMode parses text with an optional mode override:
//   - "" or "auto": auto-detect (single http(s):// without userinfo -> "url", with userinfo -> "nodes")
//   - "nodes": force parsing lines/YAML as proxy nodes (including plain http://host:port)
//   - "url": force validating single line as an HTTP/HTTPS subscription URL
func ParseProxyInputsWithMode(text, mode string) ParseResult {
	return parseProxyInputsDepth(text, strings.ToLower(strings.TrimSpace(mode)), 0)
}

func parseProxyInputsDepth(text, mode string, depth int) ParseResult {
	if mode == "" {
		mode = "auto"
	}
	res := ParseResult{
		Kind:     "unknown",
		Nodes:    []patch.ProxyNode{},
		Failures: []ParseFailure{},
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	trimmed := strings.TrimSpace(normalized)
	if trimmed == "" {
		res.Kind = "empty"
		return res
	}

	nonEmptyLines := collectNonEmptyLines(normalized)
	if mode == "url" {
		first := nonEmptyLines[0]
		if len(nonEmptyLines) == 1 && isValidSubscriptionURL(first.text) {
			res.Kind = "url"
			res.URL = first.text
			return res
		}
		res.Failures = append(res.Failures, ParseFailure{
			Line:   first.line,
			Raw:    truncateLine(first.text),
			Reason: localize.Text("请输入完整的 HTTP 或 HTTPS 订阅链接"),
		})
		return res
	}

	// Single-line http(s):// disambiguation in auto mode.
	if mode == "auto" && len(nonEmptyLines) == 1 {
		line := nonEmptyLines[0].text
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			u, err := url.Parse(line)
			if err == nil && u.Hostname() != "" {
				if u.User != nil {
					return parseURILines(nonEmptyLines, true)
				}
				if isValidSubscriptionURL(line) {
					res.Kind = "url"
					res.URL = line
					res.AmbiguousHTTP = true
					return res
				}
			}
		}
	}

	// Detect explicit URI lines before YAML (YAML would misread some strings).
	if hasProxyURILine(nonEmptyLines) {
		return parseURILines(nonEmptyLines, mode == "nodes" || len(nonEmptyLines) > 1)
	}

	// Detect Clash YAML (config document, proxy list, or single proxy map).
	if yamlRes, ok := tryParseProxyYAML(trimmed); ok {
		return yamlRes
	}

	// Try Base64 decoding and re-detect Clash YAML vs URI lines.
	if depth == 0 {
		if decoded, ok := tryDecodeBase64Text(trimmed); ok {
			sub := parseProxyInputsDepth(decoded, mode, depth+1)
			if sub.Kind != "unknown" && sub.Kind != "empty" {
				sub.Decoded = true
				return sub
			}
		}
	}

	// If caller forced node mode, attempt line-by-line parse to report exact reasons.
	if mode == "nodes" {
		return parseURILines(nonEmptyLines, true)
	}

	res.Failures = append(res.Failures, ParseFailure{
		Line:   nonEmptyLines[0].line,
		Raw:    truncateLine(nonEmptyLines[0].text),
		Reason: localize.Text("无法识别节点链接、Base64 或 Clash YAML"),
	})
	return res
}

type numberedLine struct {
	line int
	text string
}

func collectNonEmptyLines(normalized string) []numberedLine {
	var out []numberedLine
	for i, raw := range strings.Split(normalized, "\n") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		out = append(out, numberedLine{line: i + 1, text: s})
	}
	return out
}

func isValidSubscriptionURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Hostname() != "" && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && !strings.ContainsAny(raw, " \t\r\n")
}

var proxyURISchemes = map[string]bool{
	"vless": true, "vmess": true, "trojan": true,
	"ss": true, "ssr": true,
	"hysteria": true, "hysteria2": true, "hy2": true,
	"tuic": true, "wireguard": true, "wg": true,
	"socks": true, "socks5": true, "socks5h": true,
	"http": true, "https": true,
	"anytls": true, "mierus": true,
}

func lineScheme(line string) (string, bool) {
	scheme, _, ok := strings.Cut(line, "://")
	if !ok {
		return "", false
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	return scheme, proxyURISchemes[scheme]
}

func hasProxyURILine(lines []numberedLine) bool {
	for _, l := range lines {
		if strings.HasPrefix(l.text, "#") {
			continue
		}
		if _, ok := lineScheme(l.text); ok {
			return true
		}
	}
	return false
}

func parseURILines(lines []numberedLine, allowHTTPProxy bool) ParseResult {
	res := ParseResult{
		Kind:     "nodes",
		Nodes:    []patch.ProxyNode{},
		Failures: []ParseFailure{},
	}
	usedNames := map[string]bool{
		"DIRECT": true, "REJECT": true, "REJECT-DROP": true,
		"PASS": true, "COMPATIBLE": true, "PASS-RULE": true, "GLOBAL": true,
	}
	for _, entry := range lines {
		if strings.HasPrefix(entry.text, "#") {
			continue
		}
		nodes, err := parseSingleURILine(entry.text, allowHTTPProxy)
		if err != nil {
			res.Failures = append(res.Failures, ParseFailure{
				Line:   entry.line,
				Raw:    truncateLine(entry.text),
				Reason: err.Error(),
			})
			continue
		}
		for _, node := range nodes {
			node.Name = uniqueProxyNodeName(usedNames, node.Name, node.Type, node.Server, node.Port)
			res.Nodes = append(res.Nodes, node)
		}
	}
	if len(res.Nodes) == 0 && len(res.Failures) > 0 {
		res.Kind = "unknown"
	}
	return res
}

func parseSingleURILine(line string, allowHTTPProxy bool) ([]patch.ProxyNode, error) {
	scheme, ok := lineScheme(line)
	if !ok {
		return nil, fmt.Errorf("%s", localize.Text("不支持或无效的节点协议链接"))
	}
	switch scheme {
	case "wireguard", "wg":
		node, err := parseWireGuardURI(line)
		if err != nil {
			return nil, err
		}
		return []patch.ProxyNode{node}, nil
	case "http", "https":
		if !allowHTTPProxy {
			return nil, fmt.Errorf("%s", localize.Text("请指定 HTTP 代理模式或补充认证信息"))
		}
		node, err := parseHTTPOrSocksURI(line, scheme)
		if err != nil {
			return nil, err
		}
		return []patch.ProxyNode{node}, nil
	case "socks", "socks5", "socks5h":
		node, err := parseHTTPOrSocksURI(line, scheme)
		if err != nil {
			return nil, err
		}
		return []patch.ProxyNode{node}, nil
	}

	normalizedLine := normalizeSSLine(line, scheme)
	maps, err := convert.ConvertsV2Ray([]byte(normalizedLine))
	if err != nil || len(maps) == 0 {
		return nil, fmt.Errorf("%s", localize.Text("节点链接格式无效或缺少必要参数"))
	}
	var out []patch.ProxyNode
	for _, m := range maps {
		if scheme == "vless" || scheme == "vmess" {
			enrichVLinkOptions(normalizedLine, m)
		}
		node, err := mapToProxyNode(m)
		if err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, nil
}

// normalizeSSLine tolerates padded base64 in legacy ss:// links (ss://BASE64#tag).
func normalizeSSLine(line, scheme string) string {
	if scheme != "ss" {
		return line
	}
	u, err := url.Parse(line)
	if err != nil || u.Port() != "" || u.User != nil {
		return line
	}
	rawHost := strings.TrimSpace(u.Host)
	if rawHost == "" {
		return line
	}
	if decoded, err := decodeAnyBase64(rawHost); err == nil {
		text := strings.TrimSpace(string(decoded))
		if strings.Contains(text, "@") && strings.Contains(text, ":") {
			rebuilt := "ss://" + text
			if u.RawQuery != "" {
				rebuilt += "?" + u.RawQuery
			}
			if u.Fragment != "" {
				rebuilt += "#" + url.PathEscape(u.Fragment)
			}
			return rebuilt
		}
	}
	return line
}

func enrichVLinkOptions(line string, m map[string]any) {
	u, err := url.Parse(line)
	if err != nil {
		return
	}
	q := u.Query()
	for _, key := range []string{"allowInsecure", "insecure"} {
		val := strings.ToLower(strings.TrimSpace(q.Get(key)))
		if val == "1" || val == "true" {
			m["skip-cert-verify"] = true
			break
		}
	}
}

func parseHTTPOrSocksURI(line, scheme string) (patch.ProxyNode, error) {
	u, err := url.Parse(line)
	if err != nil {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("代理链接格式无效"))
	}
	server := strings.TrimSpace(u.Hostname())
	if server == "" {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("代理链接缺少服务器地址"))
	}
	portStr := u.Port()
	if portStr == "" {
		switch scheme {
		case "http":
			portStr = "80"
		case "https":
			portStr = "443"
		default:
			portStr = "1080"
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("节点端口须为 1–65535"))
	}
	typ := "socks5"
	if scheme == "http" || scheme == "https" {
		typ = "http"
	}
	opts := map[string]any{"type": typ}
	if scheme == "https" {
		opts["tls"] = true
	}
	if u.User != nil {
		username := u.User.Username()
		password, hasPass := u.User.Password()
		if !hasPass && username != "" {
			// Some clients base64-encode user:pass into the username slot.
			if decoded, err := decodeAnyBase64(username); err == nil {
				if usr, pwd, ok := strings.Cut(string(decoded), ":"); ok && utf8.ValidString(usr) && utf8.ValidString(pwd) {
					username, password = usr, pwd
				}
			}
		}
		if username != "" {
			opts["username"] = username
		}
		if password != "" {
			opts["password"] = password
		}
	}
	if typ == "socks5" {
		opts["udp"] = true
	}
	q := u.Query()
	if val := strings.ToLower(q.Get("insecure")); val == "1" || val == "true" {
		opts["skip-cert-verify"] = true
	}
	if sni := q.Get("sni"); sni != "" {
		opts["sni"] = sni
	}
	name := sanitizeNodeName(u.Fragment)
	if name == "" {
		name = fmt.Sprintf("%s-%s:%d", typ, server, port)
	}
	return patch.ProxyNode{Name: name, Type: typ, Server: server, Port: port, Options: opts}, nil
}

func parseWireGuardURI(line string) (patch.ProxyNode, error) {
	u, err := url.Parse(line)
	if err != nil {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("WireGuard 链接格式无效"))
	}
	server := strings.TrimSpace(u.Hostname())
	if server == "" {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("WireGuard 链接缺少服务器地址"))
	}
	portStr := u.Port()
	if portStr == "" {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("WireGuard 链接缺少端口"))
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("节点端口须为 1–65535"))
	}
	privKey := ""
	if u.User != nil {
		privKey = u.User.Username()
	}
	q := u.Query()
	if privKey == "" {
		privKey = firstNonEmpty(q.Get("privatekey"), q.Get("private-key"), q.Get("secretkey"))
	}
	pubKey := firstNonEmpty(q.Get("publickey"), q.Get("public-key"), q.Get("peerPublicKey"))
	if privKey == "" || pubKey == "" {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("WireGuard 链接缺少私钥或公钥"))
	}
	opts := map[string]any{
		"type":        "wireguard",
		"private-key": privKey,
		"public-key":  pubKey,
		"udp":         true,
	}
	if psk := firstNonEmpty(q.Get("presharedkey"), q.Get("preshared-key"), q.Get("psk")); psk != "" {
		opts["pre-shared-key"] = psk
	}
	if addr := firstNonEmpty(q.Get("address"), q.Get("ip")); addr != "" {
		for _, part := range strings.Split(addr, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			hostOnly, _, _ := strings.Cut(part, "/")
			if strings.Contains(hostOnly, ":") {
				if _, ok := opts["ipv6"]; !ok {
					opts["ipv6"] = hostOnly
				}
			} else {
				if _, ok := opts["ip"]; !ok {
					opts["ip"] = hostOnly
				}
			}
		}
	}
	if ipv6 := strings.TrimSpace(q.Get("ipv6")); ipv6 != "" {
		hostOnly, _, _ := strings.Cut(ipv6, "/")
		opts["ipv6"] = hostOnly
	}
	if mtuStr := strings.TrimSpace(q.Get("mtu")); mtuStr != "" {
		if mtu, err := strconv.Atoi(mtuStr); err == nil && mtu > 0 {
			opts["mtu"] = mtu
		}
	}
	if reserved := strings.TrimSpace(q.Get("reserved")); reserved != "" {
		parts := strings.Split(reserved, ",")
		if len(parts) == 3 {
			var nums []int
			ok := true
			for _, p := range parts {
				v, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil || v < 0 || v > 255 {
					ok = false
					break
				}
				nums = append(nums, v)
			}
			if ok {
				opts["reserved"] = nums
			}
		} else {
			opts["reserved"] = reserved
		}
	}
	name := sanitizeNodeName(u.Fragment)
	if name == "" {
		name = fmt.Sprintf("wireguard-%s:%d", server, port)
	}
	return patch.ProxyNode{Name: name, Type: "wireguard", Server: server, Port: port, Options: opts}, nil
}

func tryParseProxyYAML(trimmed string) (ParseResult, bool) {
	// Quick reject for strings without ":" which cannot be YAML maps.
	if !strings.Contains(trimmed, ":") {
		return ParseResult{}, false
	}
	var root any
	if err := yaml.Unmarshal([]byte(trimmed), &root); err != nil || root == nil {
		return ParseResult{}, false
	}
	usedNames := map[string]bool{
		"DIRECT": true, "REJECT": true, "REJECT-DROP": true,
		"PASS": true, "COMPATIBLE": true, "PASS-RULE": true, "GLOBAL": true,
	}

	switch v := root.(type) {
	case map[string]any:
		_, hasProxies := v["proxies"]
		_, hasGroups := v["proxy-groups"]
		_, hasRules := v["rules"]
		_, hasProviders := v["proxy-providers"]

		// Single proxy map (e.g., pasted one node block).
		if !hasProxies && !hasGroups && !hasRules && !hasProviders {
			if _, hasType := v["type"]; hasType {
				if _, hasServer := v["server"]; hasServer {
					node, err := mapToProxyNode(v)
					if err != nil {
						return ParseResult{
							Kind:     "unknown",
							Nodes:    []patch.ProxyNode{},
							Failures: []ParseFailure{{Line: 1, Raw: truncateLine(trimmed), Reason: err.Error()}},
						}, true
					}
					node.Name = uniqueProxyNodeName(usedNames, node.Name, node.Type, node.Server, node.Port)
					return ParseResult{
						Kind:     "nodes",
						Nodes:    []patch.ProxyNode{node},
						Failures: []ParseFailure{},
					}, true
				}
			}
			return ParseResult{}, false
		}

		res := ParseResult{
			Kind:     "nodes",
			Nodes:    []patch.ProxyNode{},
			Failures: []ParseFailure{},
		}
		if hasGroups || hasRules || hasProviders {
			res.Kind = "clash_yaml"
			res.YAML = trimmed
		}
		if list, ok := v["proxies"].([]any); ok {
			for i, item := range list {
				m, ok := item.(map[string]any)
				if !ok {
					res.Failures = append(res.Failures, ParseFailure{
						Line:   i + 1,
						Raw:    fmt.Sprintf("proxies[%d]", i),
						Reason: localize.Text("节点必须为 YAML 映射"),
					})
					continue
				}
				node, err := mapToProxyNode(m)
				if err != nil {
					res.Failures = append(res.Failures, ParseFailure{
						Line:   i + 1,
						Raw:    fmt.Sprintf("proxies[%d]", i),
						Reason: err.Error(),
					})
					continue
				}
				node.Name = uniqueProxyNodeName(usedNames, node.Name, node.Type, node.Server, node.Port)
				res.Nodes = append(res.Nodes, node)
			}
		}
		return res, true

	case []any:
		if len(v) == 0 {
			return ParseResult{}, false
		}
		looksLikeProxies := false
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if _, hasType := m["type"]; hasType {
					looksLikeProxies = true
					break
				}
			}
		}
		if !looksLikeProxies {
			return ParseResult{}, false
		}
		res := ParseResult{
			Kind:     "nodes",
			Nodes:    []patch.ProxyNode{},
			Failures: []ParseFailure{},
		}
		for i, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				res.Failures = append(res.Failures, ParseFailure{
					Line:   i + 1,
					Raw:    fmt.Sprintf("[%d]", i),
					Reason: localize.Text("节点必须为 YAML 映射"),
				})
				continue
			}
			node, err := mapToProxyNode(m)
			if err != nil {
				res.Failures = append(res.Failures, ParseFailure{
					Line:   i + 1,
					Raw:    fmt.Sprintf("[%d]", i),
					Reason: err.Error(),
				})
				continue
			}
			node.Name = uniqueProxyNodeName(usedNames, node.Name, node.Type, node.Server, node.Port)
			res.Nodes = append(res.Nodes, node)
		}
		if len(res.Nodes) == 0 && len(res.Failures) > 0 {
			res.Kind = "unknown"
		}
		return res, true
	}
	return ParseResult{}, false
}

func mapToProxyNode(m map[string]any) (patch.ProxyNode, error) {
	typ := strings.TrimSpace(fmt.Sprint(m["type"]))
	if m["type"] == nil || typ == "" {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("请选择节点协议"))
	}
	server := ""
	if s, ok := m["server"].(string); ok {
		server = strings.TrimSpace(s)
	}
	if server == "" || strings.IndexFunc(server, unicode.IsSpace) >= 0 || strings.IndexFunc(server, unicode.IsControl) >= 0 {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("服务器地址不能为空或包含空白"))
	}
	port, err := parsePortValue(m["port"])
	if err != nil || port < 1 || port > 65535 {
		return patch.ProxyNode{}, fmt.Errorf("%s", localize.Text("节点端口须为 1–65535"))
	}
	name := ""
	if n, ok := m["name"].(string); ok {
		name = sanitizeNodeName(n)
	}
	if name == "" {
		name = fmt.Sprintf("%s-%s:%d", typ, server, port)
	}

	opts := patch.MergeProxyOptions(nil, m)
	delete(opts, "name")
	delete(opts, "server")
	delete(opts, "port")
	opts["type"] = typ
	// Strip empty optional string values emitted by upstream converters.
	for k, v := range opts {
		if k == "type" || k == "password" || k == "uuid" || k == "cipher" {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			delete(opts, k)
		}
	}
	return patch.ProxyNode{
		Name:    name,
		Type:    typ,
		Server:  server,
		Port:    port,
		Options: opts,
	}, nil
}

func parsePortValue(v any) (int, error) {
	switch p := v.(type) {
	case int:
		return p, nil
	case int64:
		return int(p), nil
	case uint64:
		return int(p), nil
	case float64:
		return int(p), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(p))
	default:
		if v == nil {
			return 0, fmt.Errorf("missing port")
		}
		return strconv.Atoi(strings.TrimSpace(fmt.Sprint(v)))
	}
}

func sanitizeNodeName(raw string) string {
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		if r == ',' {
			return ' '
		}
		return r
	}, raw)
	s = strings.TrimSpace(s)
	if len(s) > 256 {
		s = string([]rune(s)[:256])
	}
	return s
}

func uniqueProxyNodeName(used map[string]bool, name, typ, server string, port int) string {
	base := sanitizeNodeName(name)
	if base == "" {
		base = fmt.Sprintf("%s-%s:%d", typ, server, port)
	}
	candidate := base
	for suffix := 2; used[candidate]; suffix++ {
		candidate = fmt.Sprintf("%s (%d)", base, suffix)
	}
	used[candidate] = true
	return candidate
}

func tryDecodeBase64Text(trimmed string) (string, bool) {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, trimmed)
	if len(compact) < 12 {
		return "", false
	}
	decoded, err := decodeAnyBase64(compact)
	if err != nil || len(decoded) == 0 || !utf8.Valid(decoded) {
		return "", false
	}
	for _, b := range decoded {
		if b == 0 {
			return "", false
		}
	}
	text := string(decoded)
	nonPrintable := 0
	total := 0
	for _, r := range text {
		total++
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			nonPrintable++
		}
	}
	if total == 0 || nonPrintable*10 > total {
		return "", false
	}
	return text, true
}

func decodeAnyBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func truncateLine(s string) string {
	const limit = 120
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
