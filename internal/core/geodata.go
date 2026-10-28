package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopurr/internal/geodata"
	"gopurr/internal/securefs"

	mihomoGeo "github.com/metacubex/mihomo/component/geodata"
	mihomoHTTP "github.com/metacubex/mihomo/component/http"
	"github.com/metacubex/mihomo/component/mmdb"
	"github.com/metacubex/mihomo/component/updater"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
)

type geoDatabase struct {
	Name, Path, URL string
}

type geoUpdatePlan struct {
	Enabled  bool
	Interval time.Duration
	Files    []geoDatabase
}

type geoUpdateState struct {
	Sources     string    `json:"sources"`
	LastSuccess time.Time `json:"lastSuccess"`
}

// The same lock covers config reloads and database commits. Network downloads
// happen outside it, so switching profiles is never held up by a failed source.
type geoUpdater struct {
	mu        sync.Mutex
	home      string
	state     geoUpdateState
	nextTry   time.Time
	attempted string
	wake      chan struct{}
	refresh   func(context.Context, []geoDatabase) error
}

func newGeoUpdater(home string) *geoUpdater {
	u := &geoUpdater{home: home, wake: make(chan struct{}, 1)}
	if data, err := os.ReadFile(u.statePath()); err == nil {
		_ = json.Unmarshal(data, &u.state)
	}
	u.refresh = u.update
	return u
}

func (u *geoUpdater) statePath() string { return filepath.Join(u.home, ".geo-updates.json") }

func (u *geoUpdater) plan() geoUpdatePlan {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.planLocked()
}

func (u *geoUpdater) planLocked() geoUpdatePlan {
	p := geoUpdatePlan{Enabled: updater.GeoAutoUpdate(), Interval: time.Duration(updater.GeoUpdateInterval()) * time.Hour}
	if mihomoGeo.GeoIpEnable() {
		if mihomoGeo.GeodataMode() {
			p.Files = append(p.Files, geoDatabase{"GeoIP.dat", C.Path.GeoIP(), mihomoGeo.GeoIpUrl()})
		} else {
			p.Files = append(p.Files, geoDatabase{"geoip.metadb", C.Path.MMDB(), mihomoGeo.MmdbUrl()})
		}
	}
	if mihomoGeo.GeoSiteEnable() {
		p.Files = append(p.Files, geoDatabase{"GeoSite.dat", C.Path.GeoSite(), mihomoGeo.GeoSiteUrl()})
	}
	if mihomoGeo.ASNEnable() {
		p.Files = append(p.Files, geoDatabase{"ASN.mmdb", C.Path.ASN(), mihomoGeo.ASNUrl()})
	}
	return p
}

func (u *geoUpdater) run(ctx context.Context) {
	// Unlike mihomo's CLI main, an embedded hub.Parse does not register its
	// updater. Keep one worker alive across failed initial attempts and reloads.
	u.check(ctx, time.Now(), u.plan())
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			u.check(ctx, now, u.plan())
		case <-u.wake:
			u.check(ctx, time.Now(), u.plan())
		}
	}
}

func (u *geoUpdater) check(ctx context.Context, now time.Time, p geoUpdatePlan) {
	if !p.Enabled || p.Interval <= 0 || len(p.Files) == 0 || ctx.Err() != nil {
		return
	}
	data, _ := json.Marshal(p.Files)
	sources := fmt.Sprintf("%x", sha256.Sum256(data))
	if sources == u.state.Sources && now.Before(u.state.LastSuccess.Add(p.Interval)) {
		return
	}
	if sources == u.attempted && now.Before(u.nextTry) {
		return
	}
	u.attempted = sources
	if err := u.refresh(ctx, p.Files); err != nil {
		if ctx.Err() != nil {
			return
		}
		u.nextTry = now.Add(time.Hour)
		log.Warnln("[GEO] 数据库更新失败，继续使用本地版本，一小时后重试: %s", err)
		return
	}
	u.state = geoUpdateState{Sources: sources, LastSuccess: now}
	u.nextTry = time.Time{}
	data, _ = json.Marshal(u.state)
	if err := securefs.WriteFile(u.statePath(), data, 0o600); err != nil {
		log.Warnln("[GEO] 保存更新时间失败: %s", err)
	}
	log.Infoln("[GEO] 数据库已检查，下一次更新间隔 %s", p.Interval)
}

func (u *geoUpdater) update(ctx context.Context, files []geoDatabase) error {
	var result error
	for _, file := range files {
		if err := u.updateFile(ctx, file); err != nil {
			result = errors.Join(result, fmt.Errorf("%s: %w", file.Name, err))
		}
	}
	return result
}

func (u *geoUpdater) updateFile(ctx context.Context, file geoDatabase) error {
	var data []byte
	var err error
	for _, source := range geodata.Sources(file.URL) {
		data, err = downloadGeo(ctx, source)
		if err == nil {
			err = geodata.Validate(file.Name, data)
		}
		if err == nil || ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return err
	}
	// Recheck the source after downloading: a different profile may have been
	// loaded meanwhile, including one using a custom database at the same path.
	u.mu.Lock()
	defer u.mu.Unlock()
	current := u.planLocked()
	found := false
	for _, active := range current.Files {
		if active == file {
			found = true
			break
		}
	}
	if !current.Enabled || !found {
		return errors.New("配置已切换，跳过过期的数据库更新")
	}
	old, err := os.ReadFile(file.Path)
	if err != nil {
		return err
	}
	if bytes.Equal(old, data) {
		return nil
	}
	// Windows requires mmap readers to close before the atomic replacement.
	// Reset even on a write failure so the old database can be reopened.
	switch file.Name {
	case "geoip.metadb":
		_ = mmdb.IPInstance().Reader.Close()
		defer mmdb.ReloadIP()
	case "ASN.mmdb":
		_ = mmdb.ASNInstance().Reader.Close()
		defer mmdb.ReloadASN()
	}
	if err := securefs.WriteFile(file.Path, data, 0o600); err != nil {
		return err
	}
	switch file.Name {
	case "GeoIP.dat":
		mihomoGeo.ClearGeoIPCache()
	case "GeoSite.dat":
		mihomoGeo.ClearGeoSiteCache()
	}
	return nil
}

func downloadGeo(ctx context.Context, source string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Use mihomo's own outbound routing; updates can use the working proxy
	// without depending on the OS system proxy or a fixed local port.
	resp, err := mihomoHTTP.HttpRequest(ctx, source, "GET", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	const limit = 64 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && len(data) > limit {
		err = errors.New("数据库超过 64 MB")
	}
	return data, err
}
