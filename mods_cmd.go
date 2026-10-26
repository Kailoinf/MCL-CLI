package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
	"github.com/Kailoinf/MCL-CLI/internal/modrinth"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

// mods_cmd.go is the content side of the launcher: finding a mod, resource pack
// or shader on Modrinth and putting the right file into an instance. The search
// itself is deliberately thin — one query, one page, the fields a person needs to
// decide — because the interesting part is installing the result correctly.

func (a *App) cmdSearch(args []string) int {
	f, err := parseFlags("search", args, map[string]flagSpec{
		"type":    {takesValue: true},
		"version": {takesValue: true},
		"loader":  {takesValue: true},
		"limit":   {takesValue: true},
		"offset":  {takesValue: true},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.search.usage"))
		return 0
	}
	query := strings.Join(f.args, " ")
	if strings.TrimSpace(query) == "" {
		return a.usageError("cli.missing_arg", "search", "<query>")
	}
	o := modrinth.SearchOptions{
		Type:        f.str("type", ""),
		GameVersion: f.str("version", ""),
		Loader:      f.str("loader", ""),
		Limit:       10,
	}
	if f.has("limit") {
		o.Limit, err = f.int("limit", o.Limit)
		if err != nil {
			return a.fatal(err)
		}
	}
	if f.has("offset") {
		o.Offset, err = f.int("offset", o.Offset)
		if err != nil {
			return a.fatal(err)
		}
	}
	if o.Type != "" {
		if _, ok := modrinth.DirFor(o.Type); !ok {
			return a.usageError("mod.bad_type", o.Type)
		}
	}

	res, err := modrinth.Search(a.Policy(), query, o)
	if err != nil {
		return a.fatal(fmt.Errorf("%s", i18n.T("mod.search_failed", query, err)))
	}
	if len(res.Hits) == 0 {
		a.Log.Printf("%s", i18n.T("mod.no_hits", query))
		return 0
	}
	a.Log.Printf("%s", i18n.T("mod.search_header",
		padRight("SLUG", 24), padRight("TYPE", 13), padRight("DOWNLOADS", 11),
		padRight("AUTHOR", 16), "TITLE"))
	for _, h := range res.Hits {
		a.Log.Printf("%s", i18n.T("mod.search_row",
			padRight(h.Slug, 24), padRight(h.ProjectType, 13),
			padRight(humanInt(h.Downloads), 11), padRight(trim(h.Author, 15), 16),
			trim(h.Title, 40)))
	}
	a.Log.Printf("%s", i18n.T("mod.search_footer", len(res.Hits), res.TotalHits,
		gameHint(o), offsetHint(o, len(res.Hits), res.TotalHits)))
	return 0
}

// cmdAdd installs the file of one project into an instance.
func (a *App) cmdAdd(args []string) int {
	f, err := parseFlags("add", args, map[string]flagSpec{
		"instance": {takesValue: true},
		"version":  {takesValue: true},
		"loader":   {takesValue: true},
		"type":     {takesValue: true},
		"dry-run":  {},
		"yes":      {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.add.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "add", "<project>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "add")
	}
	project := f.args[0]

	// --- which instance, and what does it run? ---------------------------
	inst, err := a.addTargetInstance(f.str("instance", ""))
	if err != nil {
		return a.fatal(err)
	}
	gameVersion, inferredLoader, err := a.instanceGameSpec(inst, f.str("version", ""), f.str("loader", ""))
	if err != nil {
		return a.fatal(err)
	}

	// --- what is this project, and which file fits? ----------------------
	meta, err := modrinth.ProjectInfo(a.Policy(), project)
	if err != nil {
		return a.fatal(fmt.Errorf("%s", i18n.T("mod.project_failed", project, err)))
	}
	projectType := meta.ProjectType
	if f.str("type", "") != "" {
		projectType = f.str("type", "")
	}
	subdir, ok := modrinth.DirFor(projectType)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("mod.type_unsupported", meta.Title, projectType)))
	}

	// Which loader to ask for: the caller's flag wins. The instance's own loader
	// only applies to mods and modpacks — a resource pack is not drawn by the
	// loader, and filtering those by "fabric" would hide every one of them. And a
	// vanilla instance cannot run a mod at all, so guessing a loader for it is how
	// people end up with a neoforge jar in a fabric instance: refuse instead.
	loader := f.str("loader", "")
	if loader == "" && needsLoader(projectType) {
		if inferredLoader == "" {
			return a.fatal(fmt.Errorf("%s", i18n.T("mod.needs_loader", inst.Name, gameVersion, gameVersion)))
		}
		loader = inferredLoader
		a.Log.Infof("%s", i18n.T("mod.loader_implied", loader))
	}
	versions, err := modrinth.Versions(a.Policy(), project, gameVersion, loader)
	if err != nil {
		return a.fatal(err)
	}
	if len(versions) == 0 {
		return a.fatal(fmt.Errorf("%s", i18n.T("mod.no_version", meta.Title, gameVersion, loaderTag(loader))))
	}
	chosen := versions[0]
	file, ok := modrinth.PickFile(chosen)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("mod.no_file", meta.Title)))
	}

	dir := filepath.Join(a.instanceDir(inst.Name), subdir)
	dest := filepath.Join(dir, filepath.Base(file.Filename))
	// One candidate: the CDN URL the API handed us. Modrinth is not mirrored by
	// BMCLAPI, so there is no second source to fall back to.
	item := dl.Item{
		Candidates: []mirror.Candidate{{URL: file.URL, Source: "modrinth"}},
		Dest:       dest,
		SHA1:       file.Hashes.SHA1,
		Size:       file.Size,
		Desc:       meta.Title,
	}

	a.Log.Printf("%s", i18n.T("mod.picked", meta.Title, chosen.VersionNumber))
	a.Log.Printf("%s", i18n.T("mod.target", inst.Name, subdir, dest))
	for _, dep := range chosen.Dependencies {
		if dep.DependencyType == "required" {
			a.Log.Printf("%s", i18n.T("mod.dependency", dep.ProjectID))
		}
	}

	// already there and intact: nothing to do
	if st, err := os.Stat(dest); err == nil && (file.Size == 0 || st.Size() == file.Size) && item.Verify() {
		a.Log.Printf("%s", i18n.T("mod.already", filepath.Base(dest)))
		return 0
	}
	if dryRun := f.boolean("dry-run"); dryRun {
		a.Log.Printf("%s", i18n.T("mod.would_fetch", filepath.Base(dest), humanBytes(file.Size)))
		a.Log.Printf("%s", i18n.T("mcv.dry_run"))
		return 0
	}
	if !a.Yeseverything {
		ok, err := a.confirmFile(meta.Title, file.Size)
		if err != nil {
			return a.fatal(err)
		}
		if !ok {
			return a.fatal(fmt.Errorf("%s", i18n.T("dl.declined", meta.Title)))
		}
	}
	if err := dl.FetchAll(a.Policy(), []dl.Item{item}, 1, a.Log); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("mod.installed", filepath.Base(dest), inst.Name, subdir))
	return 0
}

// cmdMods lists what an instance already has in its content directories.
func (a *App) cmdMods(args []string) int {
	f, err := parseFlags("mods", args, map[string]flagSpec{})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.mods.usage"))
		return 0
	}
	name := ""
	if len(f.args) > 0 {
		name = f.args[0]
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "mods")
	}
	inst, err := a.addTargetInstance(name)
	if err != nil {
		return a.fatal(err)
	}
	dir := a.instanceDir(inst.Name)
	anything := false
	for _, sub := range []string{"mods", "resourcepacks", "shaderpacks", "datapacks"} {
		files := listContent(filepath.Join(dir, sub))
		if len(files) == 0 {
			continue
		}
		anything = true
		a.Log.Printf("%s", i18n.T("mod.list_header", inst.Name, sub))
		for _, f := range files {
			a.Log.Printf("%s", i18n.T("mod.list_row", padRight(f.name, 48), humanBytes(f.size)))
		}
	}
	if !anything {
		a.Log.Printf("%s", i18n.T("mod.list_empty", inst.Name))
	}
	return 0
}

// addTargetInstance picks the instance to install into: the named one, or the
// only instance there is — asking for a name when there is exactly one candidate
// would be noise.
func (a *App) addTargetInstance(name string) (*store.Instance, error) {
	if name != "" {
		inst, ok := a.Store.GetInstance(name)
		if !ok {
			return nil, fmt.Errorf("%s", i18n.T("inst.not_found", name))
		}
		return inst, nil
	}
	names := a.Store.InstanceNames()
	switch len(names) {
	case 0:
		return nil, fmt.Errorf("%s", i18n.T("mod.no_instance"))
	case 1:
		inst, _ := a.Store.GetInstance(names[0])
		a.Log.Infof("%s", i18n.T("mod.only_instance", names[0]))
		return inst, nil
	}
	return nil, fmt.Errorf("%s", i18n.T("mod.pick_instance", strings.Join(names, ", ")))
}

// needsLoader reports whether a project type only makes sense with a mod loader.
func needsLoader(projectType string) bool {
	switch strings.ToLower(strings.TrimSpace(projectType)) {
	case "mod", "modpack":
		return true
	}
	return false
}

// loaderTag renders a loader for a message, or says so when there is none.
func loaderTag(loader string) string {
	if loader == "" {
		return ""
	}
	return "/" + loader
}

// instanceGameSpec works out which game version the instance runs and which loader
// it has, unless the caller already said.
func (a *App) instanceGameSpec(inst *store.Instance, version, loader string) (string, string, error) {
	game := version
	modLoader := loader
	if game != "" && modLoader != "" {
		return game, modLoader, nil
	}
	v, err := a.Loader(true).Lookup(inst.Version, "")
	if err != nil {
		if game == "" {
			return "", "", err
		}
		return game, modLoader, nil
	}
	eff := v.Effective()
	if game == "" {
		// a modded profile inherits from the game version it loads
		game = eff.ID
		if eff.InheritsFrom != "" {
			game = eff.InheritsFrom
		}
	}
	if modLoader == "" && strings.HasPrefix(inst.Version, "fabric-loader-") {
		modLoader = "fabric"
	}
	return game, modLoader, nil
}

type contentFile struct {
	name string
	size int64
}

func listContent(dir string) []contentFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []contentFile
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, contentFile{name: e.Name(), size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// trim shortens a table cell without cutting a rune in half.
func trim(s string, n int) string {
	if displayWidth(s) <= n {
		return s
	}
	runes := []rune(s)
	var out strings.Builder
	width := 0
	for _, r := range runes {
		w := runeWidth(r)
		if width+w > n-1 {
			break
		}
		out.WriteRune(r)
		width += w
	}
	return out.String() + "…"
}

// humanInt prints a count with thousands separators, which is what makes a
// downloads column readable.
func humanInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	return string(out)
}

func gameHint(o modrinth.SearchOptions) string {
	parts := []string{}
	if o.GameVersion != "" {
		parts = append(parts, o.GameVersion)
	}
	if o.Loader != "" {
		parts = append(parts, o.Loader)
	}
	if len(parts) == 0 {
		return i18n.T("mod.hint_any")
	}
	return strings.Join(parts, " / ")
}

func offsetHint(o modrinth.SearchOptions, shown, total int) string {
	if shown+o.Offset >= total {
		return ""
	}
	return i18n.T("mod.hint_more", o.Offset+shown)
}
