package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/auth"
	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
	"github.com/Kailoinf/MCL-CLI/internal/paths"
	"github.com/Kailoinf/MCL-CLI/internal/secret"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

// Version is the mcli version reported by `mcli version`.
var Version = "0.2.0"

// LauncherName is handed to the game as -Dminecraft.launcher.brand.
const LauncherName = "mcli"

// App is the shared command context.
type App struct {
	Paths paths.Paths
	Store *store.Store
	Log   *logx.Logger

	Yeseverything bool // --yes: never prompt
	DryRun        bool

	// Health is shared by every download policy in a run, so a mirror that
	// starts stalling is demoted once instead of once per file.
	Health *dl.SourceHealth

	started time.Time
}

// newApp loads paths, mcli.toml, the language and the logger. Global flags are
// applied here, which is why language is re-applied after the store is read.
func newApp(glob *flags) (*App, error) {
	p, err := paths.Resolve()
	if err != nil {
		return nil, err
	}
	if err := p.Ensure(); err != nil {
		return nil, err
	}
	configPath := p.Config()
	if v, ok := glob.values["config"]; ok && v != "" {
		configPath = v
	}
	dir := filepath.Dir(configPath)
	st, err := store.Open(store.Files{
		Config:  configPath,
		State:   filepath.Join(dir, paths.StateFile),
		Refresh: filepath.Join(dir, paths.RefreshFile),
		Session: filepath.Join(dir, paths.SessionFile),
		Last:    filepath.Join(dir, paths.LastFile),
	})
	if err != nil {
		return nil, err
	}

	// language: --lang wins, then mcli.toml, then the environment
	lang := st.Cfg().Language
	if v, ok := glob.values["lang"]; ok && v != "" {
		lang = v
	}
	if err := i18n.Set(lang); err != nil {
		return nil, err
	}

	termLevel, err := logx.ParseLevel(st.Cfg().LogLevelTerminal)
	if err != nil {
		return nil, err
	}
	fileLevel, err := logx.ParseLevel(st.Cfg().LogLevelFile)
	if err != nil {
		return nil, err
	}
	if glob.boolean("quiet") {
		termLevel = logx.LevelError
	}
	if glob.boolean("verbose") {
		termLevel = logx.LevelDebug
	}
	log, err := logx.New(termLevel, fileLevel, p.LogFile())
	if err != nil {
		return nil, err
	}

	cfg := st.Cfg()
	if cfg.JavaDir == "" {
		cfg.JavaDir = p.JDKDir()
		if err := st.SaveConfigValue("java_dir"); err != nil {
			log.Warnf("%s", i18n.T("err.state.no_save", err))
		}
	}
	if st.Data.StateVersion > store.StateVersion {
		log.Warnf("%s", i18n.T("state.too_new", st.StatePath, st.Data.StateVersion, store.StateVersion))
	}
	for _, key := range st.UnknownKeys {
		log.Warnf("%s", i18n.T("state.unknown_key", key))
	}

	app := &App{
		Paths:         p,
		Store:         st,
		Log:           log,
		Yeseverything: glob.boolean("yes"),
		Health:        dl.NewSourceHealth(3),
		started:       time.Now(),
	}
	return app, nil
}

// Close flushes logs.
func (a *App) Close() { a.Log.Close() }

// Mapper builds the download URL mapper from the current config.
func (a *App) Mapper() mirror.Mapper {
	cfg := a.Store.Cfg()
	return mirror.Mapper{Endpoint: cfg.BmclapiEndpoint, Fallback: cfg.MirrorFallback}
}

// Policy builds the download policy from the current config.
func (a *App) Policy() dl.Policy {
	cfg := a.Store.Cfg()
	return dl.Policy{
		Retries: cfg.DownloadRetryTimes,
		Timeout: time.Duration(cfg.HTTPTimeout) * time.Second,
		LargeMB: cfg.LargeDownloadMB,
		Log:     a.Log,
		Health:  a.Health,
		Confirm: a.confirmFile,
	}
}

// MinecraftDir is the game root.
func (a *App) MinecraftDir() string { return a.Store.Cfg().MinecraftDir }

// VersionsDir is the version/instance directory inside the game root.
func (a *App) VersionsDir() string {
	dir := a.Store.Cfg().VersionsDir
	if dir == "" {
		dir = "versions"
	}
	return filepath.Join(a.MinecraftDir(), dir)
}

// ManifestStore wires the manifest cache to state.toml.
func (a *App) ManifestStore() *mcv.ManifestStore {
	return &mcv.ManifestStore{
		Store:     a.Store,
		Mapper:    a.Mapper(),
		Policy:    a.Policy(),
		Log:       a.Log,
		CacheFile: a.Paths.ManifestFile(),
	}
}

// ClientID is the OAuth client id used for the device-code flow.
func (a *App) ClientID() string {
	if id := a.Store.Cfg().AuthClientID; id != "" {
		return id
	}
	return auth.ClientID
}

// Loader wires the version JSON loader.
func (a *App) Loader(dryRun bool) mcv.Loader {
	return mcv.Loader{
		Dir:    a.VersionsDir(),
		Mapper: a.Mapper(),
		Policy: a.Policy(),
		Log:    a.Log,
		DryRun: dryRun,
	}
}

// Installer wires the version installer.
func (a *App) Installer(dryRun bool) mcv.Installer {
	return mcv.Installer{
		Root:        a.MinecraftDir(),
		VersionsDir: a.Store.Cfg().VersionsDir,
		Mapper:      a.Mapper(),
		Policy:      a.Policy(),
		Log:         a.Log,
		Env:         mcv.EnvCurrent(mcv.FeatureSet(false, nil)),
		DryRun:      dryRun,
		Workers:     6,
		ConfirmTotal: func(desc string, files int, total int64) (bool, error) {
			return a.confirmBatch(desc, files, total)
		},
	}
}

// JavaInstaller wires the runtime installer. force reinstalls from scratch.
func (a *App) JavaInstaller(force bool) java.Installer {
	return java.Installer{
		Mapper:   a.Mapper(),
		Policy:   a.Policy(),
		Log:      a.Log,
		DestRoot: a.Store.Cfg().JavaDir,
		Workers:  6,
		Force:    force,
		Confirm: func(desc string, files int, total int64) (bool, error) {
			return a.confirmBatch(desc, files, total)
		},
	}
}

// AuthClient builds the Microsoft login client. A configured proxy is honoured
// through the standard environment variables.
func (a *App) AuthClient(timeout time.Duration) *auth.Client {
	if timeout <= 0 {
		timeout = time.Duration(a.Store.Cfg().HTTPTimeout) * time.Second
	}
	client := auth.NewClient(timeout, a.Log)
	client.ClientID = a.ClientID()
	// The default client id only supports the browser (authorization code)
	// flow, so refresh tokens must go back to the endpoint that issued them.
	client.UseBrowserFlow()
	return client
}

// ---------------------------------------------------------------------------
// prompting

// confirmFile asks about a single large file.
func (a *App) confirmFile(desc string, size int64) (bool, error) {
	if a.Yeseverything || !a.Store.Cfg().ConfirmLargeDownload {
		return true, nil
	}
	return a.prompt(i18n.T("dl.confirm_large", desc, humanBytes(size)))
}

// confirmBatch asks once about a batch of downloads.
func (a *App) confirmBatch(desc string, files int, total int64) (bool, error) {
	if a.Yeseverything || !a.Store.Cfg().ConfirmLargeDownload {
		return true, nil
	}
	if total < int64(a.Store.Cfg().LargeDownloadMB)*1024*1024 {
		return true, nil
	}
	return a.prompt(i18n.T("dl.confirm_batch", desc, files, humanBytes(total)))
}

// prompt prints a question and reads y/n from stdin.
func (a *App) prompt(question string) (bool, error) {
	fmt.Fprintf(os.Stdout, "\n%s%s", question, i18n.T("common.confirm_suffix"))
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "是":
		return true, nil
	}
	return false, nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	i := -1
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f%s", v, units[i])
}

// ---------------------------------------------------------------------------
// accounts

// activeAccount returns the selected account or an error when there is none.
func (a *App) activeAccount() (*store.Account, error) {
	acc := a.Store.ActiveAccount()
	if acc == nil {
		if len(a.Store.Data.Accounts) == 0 {
			return nil, fmt.Errorf("%s", i18n.T("auth.no_account"))
		}
		names := a.Store.AccountNames()
		acc = a.Store.Data.Accounts[names[0]]
		a.Store.SetActive(acc.UUID)
		if err := a.Store.Save(); err != nil {
			return nil, err
		}
	}
	return acc, nil
}

// session returns a usable Minecraft session, refreshing the token when the
// stored one is close to expiry.
func (a *App) session(acc *store.Account) (*store.Account, *auth.Session, error) {
	key, err := a.Store.AccountKey(acc)
	if err != nil {
		return nil, nil, err
	}
	_, stored := a.Store.Tokens(acc.UUID)
	client := a.AuthClient(0)
	if stored.MCTokenEnc != "" && time.Until(stored.MCTokenExp) > 5*time.Minute {
		mcToken, err := secret.Decrypt(key, stored.MCTokenEnc)
		if err == nil {
			prof, err := client.MineProfile(string(mcToken))
			if err == nil {
				if prof.Name != "" {
					acc.Name = prof.Name
				}
				if prof.ID != "" {
					acc.UUID = prof.ID
				}
				return acc, &auth.Session{
					MC:      auth.MCTokens{AccessToken: string(mcToken), ExpiresAt: stored.MCTokenExp},
					Profile: *prof,
					XUID:    acc.XUID,
				}, nil
			}
			a.Log.Debugf("%s", i18n.T("auth.refreshing"))
		}
	}
	refreshEnc, err := secret.Decrypt(key, a.Store.Data.Refresh[acc.UUID].RefreshEnc)
	if err != nil || len(refreshEnc) == 0 {
		return nil, nil, fmt.Errorf("%s", i18n.T("auth.refresh_failed", err))
	}
	sess, err := client.RefreshSession(string(refreshEnc))
	if err != nil {
		return nil, nil, err
	}
	if err := a.storeSession(acc, sess, ""); err != nil {
		return nil, nil, err
	}
	return acc, sess, nil
}

// storeSession persists fresh credentials and the profile identity: identity to
// state.toml, the refresh token to refresh.toml, the Minecraft token to
// session.toml. Each credential gets its own file so a short-lived token never
// sits in the same document as the long-lived one.
func (a *App) storeSession(acc *store.Account, sess *auth.Session, label string) error {
	if err := a.Store.SetAccountSalt(acc); err != nil {
		return err
	}
	key, err := a.Store.AccountKey(acc)
	if err != nil {
		return err
	}
	refreshEnc, err := secret.Encrypt(key, []byte(sess.MS.RefreshToken))
	if err != nil {
		return err
	}
	mcEnc, err := secret.Encrypt(key, []byte(sess.MC.AccessToken))
	if err != nil {
		return err
	}
	oldUUID := acc.UUID
	if label != "" {
		acc.Label = label
	}
	acc.Name = sess.Profile.Name
	acc.UUID = sess.Profile.ID
	acc.XUID = sess.XUID
	acc.LastLogin = time.Now()
	if acc.AddedAt.IsZero() {
		acc.AddedAt = time.Now()
	}
	if acc.Label == "" {
		acc.Label = "ms:" + acc.Name
	}
	if oldUUID != "" && oldUUID != acc.UUID {
		a.Store.DropAccount(oldUUID)
	}
	tok, session := a.Store.Tokens(acc.UUID)
	tok.RefreshEnc = refreshEnc
	session.MCTokenEnc = mcEnc
	session.MCTokenExp = sess.MC.ExpiresAt
	a.Store.Data.Accounts[acc.UUID] = acc
	a.Store.SetActive(acc.UUID)
	if err := a.Store.Save(); err != nil {
		return err
	}
	return a.Store.SaveTokens()
}

// authFor builds the placeholder auth block for a launch.
func authFor(acc *store.Account, sess *auth.Session, clientID string) mcv.Auth {
	if clientID == "" {
		clientID = auth.ClientID
	}
	return mcv.Auth{
		PlayerName:  sess.Profile.Name,
		UUID:        strings.ReplaceAll(sess.Profile.ID, "-", ""),
		AccessToken: sess.MC.AccessToken,
		XUID:        sess.XUID,
		ClientID:    clientID,
		UserType:    "msa",
		Properties:  "{}",
	}
}
