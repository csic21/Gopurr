package mesh

import "gopurr/internal/localize"

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ParseServices accepts explicit ports only, never ranges or wildcards.
func ParseServices(raw string) ([]Service, error) {
	var out []Service
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '\n' }) {
		p := strings.Split(strings.ToLower(part), "/")
		n, err := strconv.ParseUint(p[0], 10, 16)
		if err != nil || n == 0 || len(p) > 2 {
			return nil, errors.New(localize.Text("请输入明确端口，例如 22/tcp, 3389/tcp；不支持端口范围"))
		}
		protocol := "tcp"
		if len(p) == 2 {
			protocol = p[1]
		}
		if protocol != "tcp" && protocol != "udp" {
			return nil, errors.New(localize.Text("协议只能是 tcp 或 udp"))
		}
		s := Service{Protocol: protocol, Port: uint16(n)}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
		if len(out) > 16 {
			return nil, errors.New(localize.Text("最多授权 16 个服务端口"))
		}
	}
	return out, nil
}

func FormatServices(services []Service) string {
	var parts []string
	for _, s := range services {
		parts = append(parts, fmt.Sprintf("%d/%s", s.Port, s.Protocol))
	}
	return strings.Join(parts, ", ")
}
