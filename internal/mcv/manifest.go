package mcv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

// Manifest is the version manifest: every version Mojang has ever published.
type Manifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []ManifestVersion `json:"versions"`
}

// ManifestVersion is one entry of the manifest.
type ManifestVersion struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"` // release | snapshot | old_beta | old_alpha
	URL             string    `json:"url"`
	Time            time.Time `json:"time"`
	ReleaseTime     time.Time `json:"releaseTime"`
	SHA1            string    `json:"sha1"`
	ComplianceLevel int       `json:"complianceLevel"`
}

// Find returns the manifest entry for a version id.
func (m *Manifest) Find(id string) (ManifestVersion, bool) {
	for _, v := range m.Versions {
		if v.ID == id {
			return v, true
		}
	}
	return ManifestVersion{}, false
}

// Filter returns entries matching an optional type filter and search text,
// newest first, capped at limit (0 = no cap).
func (m *Manifest) Filter(kind, search string, limit int) []ManifestVersion {
	out := make([]ManifestVersion, 0, len(m.Versions))
	needle := strings.ToLower(search)
	for _, v := range m.Versions {
		if kind != "" && !strings.EqualFold(v.Type, kind) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(v.ID), needle) {
			continue
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ReleaseTime.After(out[j].ReleaseTime) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ManifestStore caches the manifest on disk (raw JSON under cache/) and keeps
// only its metadata in state.toml, so the TOML documents stay small.
type ManifestStore struct {
	DB     *store.DB
	Mapper mirror.Mapper
	Policy dl.Policy
	Log    *logx.Logger
	// CacheFile is where the raw manifest JSON lives.
	CacheFile string
}

// Get returns the manifest, refreshing it when the cache is missing or older
// than the configured TTL. force skips the TTL check.
func (m *ManifestStore) Get(force bool) (*Manifest, error) {
	cfg := m.DB.Cfg()
	cache := m.DB.Data.Manifest
	ttl := time.Duration(cfg.ManifestTTLHours) * time.Hour
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	raw, cacheErr := m.readCache()
	haveCache := cache != nil && cacheErr == nil

	if haveCache && !force {
		age := time.Since(cache.FetchedAt)
		if age < ttl {
			m.log().Debugf("%s", i18n.T("mcv.manifest_cached", age.Round(time.Minute)))
			return decodeManifest(raw)
		}
		if !cfg.ManifestAutoRefresh {
			m.log().Warnf("%s", i18n.T("mcv.manifest_expired"))
			return decodeManifest(raw)
		}
		m.log().Infof("%s", i18n.T("mcv.manifest_expired"))
	}
	man, source, fresh, err := m.fetch()
	if err != nil {
		if haveCache {
			m.log().Warnf("%s", i18n.T("mcv.manifest_failed", err))
			return decodeManifest(raw)
		}
		return nil, fmt.Errorf("%s", i18n.T("mcv.manifest_failed", err))
	}
	if m.CacheFile != "" {
		if err := writeRaw(m.CacheFile, fresh); err != nil {
			m.log().Warnf("%s", err)
		}
	}
	m.DB.Data.Manifest = &store.ManifestCache{
		Source:    source,
		FetchedAt: time.Now(),
		File:      m.CacheFile,
	}
	if err := m.DB.Save(); err != nil {
		m.log().Warnf("%s", i18n.T("err.db.no_save", err))
	}
	m.log().Infof("%s", i18n.T("mcv.manifest_refreshed", source, len(man.Versions)))
	return man, nil
}

// readCache returns the cached manifest body, if it is still there.
func (m *ManifestStore) readCache() ([]byte, error) {
	path := m.CacheFile
	if path == "" && m.DB.Data.Manifest != nil {
		path = m.DB.Data.Manifest.File
	}
	if path == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(path)
}

func writeRaw(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *ManifestStore) fetch() (*Manifest, string, json.RawMessage, error) {
	m.log().Infof("%s", i18n.T("mcv.manifest_refreshing"))
	data, source, err := m.Policy.FetchBytes(m.Mapper.Manifest(), "version_manifest_v2.json")
	if err != nil {
		return nil, "", nil, err
	}
	man, err := decodeManifest(data)
	if err != nil {
		return nil, "", nil, err
	}
	return man, source, json.RawMessage(data), nil
}

// Refresh forces a manifest update (used by `mcli refresh`).
func (m *ManifestStore) Refresh() (*Manifest, error) { return m.Get(true) }

func decodeManifest(raw []byte) (*Manifest, error) {
	man := &Manifest{}
	if err := json.Unmarshal(raw, man); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("mcv.manifest_failed", err))
	}
	return man, nil
}

func (m *ManifestStore) log() *logx.Logger {
	if m.Log != nil {
		return m.Log
	}
	return logx.Discard()
}
