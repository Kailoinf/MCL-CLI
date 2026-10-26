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
var Version = "0.1.0"

// LauncherName is handed to the game as -Dminecraft.launcher.brand.
const LauncherName = "mcli"

// App is the shared command context.
type App struct {
	Paths paths.Paths
	DB    *store.DB
	Log   *logx.Logger

	Yeseverything bool // --yes: never prompt
	DryRun        bool

	started time.Time
}

// newApp loads paths, mcli.db, the language and the logger. Global flags are
// applied here, which is why language is re-applied after the DB is read.
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
	statePath := filepath.Join(filepath.Dir(configPath), paths.StateFile)
	lastPath := filepath.Join(filepath.Dir(configPath), paths.LastFile)
	db, err := store.Open(configPath, statePath, lastPath)
	if err != nil {
		return nil, err
	}

	// language: --lang wins, then mcli.db, then the environment
	lang := db.Cfg().Language
	if v, ok := glob.values["lang"]; ok && v != "" {
		lang = v
	}
	if err := i18n.Set(lang); err != nil {
		return nil, err
	}

	termLevel, err := logx.ParseLevel(db.Cfg().LogLevelTerminal)
	if err != nil {
		return nil, err
	}
	fileLevel, err := logx.ParseLevel(db.Cfg().LogLevelFile)
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

	cfg := db.Cfg()
	if cfg.JavaDir == "" {
		cfg.JavaDir = p.JDKDir()
		if err := db.SaveConfigValue("java_dir"); err != nil {
			log.Warnf("%s", i18n.T("err.db.no_save", err))
		}
	}
	if from := db.MigratedFrom(); from != "" {
		log.Infof("%s", i18n.T("db.migrated", from, db.Path, db.StatePath))
	}
	for _, key := range db.UnknownKeys {
		log.Warnf("%s", i18n.T("db.unknown_key", key))
	}

	app := &App{
		Paths:         p,
		DB:            db,
		Log:           log,
		Yeseverything: glob.boolean("yes"),
		started:       time.Now(),
	}
	return app, nil
}

// Close flushes logs.
func (a *App) Close() { a.Log.Close() }

// Mapper builds the download URL mapper from the current config.
func (a *App) Mapper() mirror.Mapper {
	cfg := a.DB.Cfg()
	return mirror.Mapper{Endpoint: cfg.BmclapiEndpoint, Fallback: cfg.MirrorFallback}
}

// Policy builds the download policy from the current config.
func (a *App) Policy() dl.Policy {
	cfg := a.DB.Cfg()
	return dl.Policy{
		Retries: cfg.DownloadRetryTimes,
		Timeout: time.Duration(cfg.HTTPTimeout) * time.Second,
		LargeMB: cfg.LargeDownloadMB,
		Log:     a.Log,
		Confirm: a.confirmFile,
	}
}

// MinecraftDir is the game root.
func (a *App) MinecraftDir() string { return a.DB.Cfg().MinecraftDir }

// VersionsDir is the version/instance directory inside the game root.
func (a *App) VersionsDir() string {
	dir := a.DB.Cfg().VersionsDir
	if dir == "" {
		dir = "versions"
	}
	return filepath.Join(a.MinecraftDir(), dir)
}

// ManifestStore wires the manifest cache to mcli.db.
func (a *App) ManifestStore() *mcv.ManifestStore {
	return &mcv.ManifestStore{
		DB:        a.DB,
		Mapper:    a.Mapper(),
		Policy:    a.Policy(),
		Log:       a.Log,
		CacheFile: a.Paths.ManifestFile(),
	}
}

// ClientID is the OAuth client id used for the device-code flow.
func (a *App) ClientID() string {
	if id := a.DB.Cfg().AuthClientID; id != "" {
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
		VersionsDir: a.DB.Cfg().VersionsDir,
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

// JavaInstaller wires the runtime installer.
func (a *App) JavaInstaller() java.Installer {
	return java.Installer{
		Mapper:   a.Mapper(),
		Policy:   a.Policy(),
		Log:      a.Log,
		DestRoot: a.DB.Cfg().JavaDir,
		Workers:  6,
		Confirm: func(desc string, files int, total int64) (bool, error) {
			return a.confirmBatch(desc, files, total)
		},
	}
}

// AuthClient builds the Microsoft login client. A configured proxy is honoured
// through the standard environment variables.
func (a *App) AuthClient(timeout time.Duration) *auth.Client {
	if timeout <= 0 {
		timeout = time.Duration(a.DB.Cfg().HTTPTimeout) * time.Second
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
	if a.Yeseverything || !a.DB.Cfg().ConfirmLargeDownload {
		return true, nil
	}
	return a.prompt(i18n.T("dl.confirm_large", desc, humanBytes(size)))
}

// confirmBatch asks once about a batch of downloads.
func (a *App) confirmBatch(desc string, files int, total int64) (bool, error) {
	if a.Yeseverything || !a.DB.Cfg().ConfirmLargeDownload {
		return true, nil
	}
	if total < int64(a.DB.Cfg().LargeDownloadMB)*1024*1024 {
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
	acc := a.DB.ActiveAccount()
	if acc == nil {
		if len(a.DB.Data.Accounts) == 0 {
			return nil, fmt.Errorf("%s", i18n.T("auth.no_account"))
		}
		names := a.DB.AccountNames()
		acc = a.DB.Data.Accounts[names[0]]
		a.DB.SetActive(acc.UUID)
		if err := a.DB.Save(); err != nil {
			return nil, err
		}
	}
	return acc, nil
}

// session returns a usable Minecraft session, refreshing the token when the
// stored one is close to expiry.
func (a *App) session(acc *store.Account) (*store.Account, *auth.Session, error) {
	key, err := a.DB.AccountKey(acc)
	if err != nil {
		return nil, nil, err
	}
	client := a.AuthClient(0)
	if acc.MCTokenEnc != "" && time.Until(acc.MCTokenExp) > 5*time.Minute {
		mcToken, err := secret.Decrypt(key, acc.MCTokenEnc)
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
					MC:      auth.MCTokens{AccessToken: string(mcToken), ExpiresAt: acc.MCTokenExp},
					Profile: *prof,
					XUID:    acc.XUID,
				}, nil
			}
			a.Log.Debugf("%s", i18n.T("auth.refreshing"))
		}
	}
	refreshEnc, err := secret.Decrypt(key, acc.RefreshEnc)
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

// storeSession persists fresh tokens (encrypted) and the profile identity.
func (a *App) storeSession(acc *store.Account, sess *auth.Session, label string) error {
	if err := a.DB.SetAccountSalt(acc); err != nil {
		return err
	}
	key, err := a.DB.AccountKey(acc)
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
	acc.RefreshEnc = refreshEnc
	acc.MCTokenEnc = mcEnc
	acc.MCTokenExp = sess.MC.ExpiresAt
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
		delete(a.DB.Data.Accounts, oldUUID)
	}
	a.DB.Data.Accounts[acc.UUID] = acc
	a.DB.SetActive(acc.UUID)
	return a.DB.Save()
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
