// Package geodata supplies the offline baseline used before the proxy can
// download current databases. Existing valid databases always take precedence.
package geodata

import (
	"bytes"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopurr/internal/securefs"

	mihomoGeo "github.com/metacubex/mihomo/component/geodata"
	_ "github.com/metacubex/mihomo/component/geodata/standard"
	"github.com/oschwald/maxminddb-golang"
)

//go:embed data/*.gz
var baseline embed.FS

// Files maps runtime filenames to the names published by MetaCubeX. Embedding
// them in the executable also covers protected macOS and Windows service copies.
var Files = map[string]string{
	"geoip.metadb": "geoip.metadb",
	"GeoIP.dat":    "geoip.dat",
	"GeoSite.dat":  "geosite.dat",
	"ASN.mmdb":     "GeoLite2-ASN.mmdb",
}

const upstream = "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/release/"

// Defaults keeps advanced profile/YAML choices while enabling daily updates in
// ordinary subscriptions, which usually omit these settings altogether.
func Defaults(doc map[string]any) {
	if _, ok := doc["geo-auto-update"]; !ok {
		doc["geo-auto-update"] = true
	}
	if _, ok := doc["geo-update-interval"]; !ok {
		doc["geo-update-interval"] = 24
	}
	urls, _ := doc["geox-url"].(map[string]any)
	if urls == nil {
		urls = map[string]any{}
		doc["geox-url"] = urls
	}
	for key, file := range map[string]string{"mmdb": "geoip.metadb", "geoip": "geoip.dat", "geosite": "geosite.dat", "asn": "GeoLite2-ASN.mmdb"} {
		if _, ok := urls[key]; !ok {
			urls[key] = upstream + file
		}
	}
}

// Sources adds the upstream project's documented CDN only for its own default
// URLs. A custom database source must never silently become a different dataset.
func Sources(url string) []string {
	urls := []string{url}
	for _, file := range Files {
		if url == upstream+file || url == "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/"+file {
			return append(urls, "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/"+file)
		}
	}
	return urls
}

func Bundled(name string) ([]byte, error) {
	compressed, err := baseline.ReadFile("data/" + name + ".gz")
	if err != nil {
		return nil, err
	}
	r, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// Validate checks downloaded content before replacing a working database.
func Validate(name string, data []byte) error {
	switch name {
	case "geoip.metadb", "ASN.mmdb":
		r, err := maxminddb.FromBytes(data)
		if err != nil {
			return err
		}
		defer r.Close()
		// Meta's compact MMDB intentionally omits optional MaxMind metadata;
		// Reader.Verify rejects it even though mihomo can read it normally.
		return nil
	case "GeoIP.dat", "GeoSite.dat":
		loader, err := mihomoGeo.GetGeoDataLoader("standard")
		if err != nil {
			return err
		}
		if name == "GeoIP.dat" {
			_, err = loader.LoadIPByBytes(data, "cn")
		} else {
			_, err = loader.LoadSiteByBytes(data, "cn")
		}
		return err
	default:
		return fmt.Errorf("未知数据库: %s", name)
	}
}

// Ensure repairs a missing/truncated cache without network access. It runs in
// the daemon, so a brand-new privileged service home is covered as well.
func Ensure(home string) error {
	if err := securefs.PrivateDir(home); err != nil {
		return err
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return err
	}
	for name := range Files {
		target := name
		for _, entry := range entries {
			match := strings.EqualFold(entry.Name(), name)
			if name == "geoip.metadb" {
				match = match || strings.EqualFold(entry.Name(), "Country.mmdb") || strings.EqualFold(entry.Name(), "geoip.db")
			}
			if match {
				target = entry.Name()
				break
			}
		}
		path := filepath.Join(home, target)
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("数据库不是普通文件: %s", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if Validate(name, data) == nil {
				continue
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		data, err := Bundled(name)
		if err != nil {
			return err
		}
		if err := Validate(name, data); err != nil {
			return fmt.Errorf("随包数据库 %s 无效: %w", name, err)
		}
		if err := securefs.WriteFile(path, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
