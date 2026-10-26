package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
	"github.com/Kailoinf/MCL-CLI/internal/modrinth"
	"github.com/Kailoinf/MCL-CLI/internal/mrpack"
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
		"instance":     {takesValue: true},
		"version":      {takesValue: true},
		"loader":       {takesValue: true},
		"type":         {takesValue: true},
		"pack-version": {takesValue: true},
		"dry-run":      {},
		"yes":          {},
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
	spec, err := a.instanceSpecOf(inst, f.str("version", ""), f.str("loader", ""))
	if err != nil {
		return a.fatal(err)
	}
	gameVersion, inferredLoader := spec.Game, spec.Loader

	// --- what is this project, and which file fits? ----------------------
	meta, err := modrinth.ProjectInfo(a.Policy(), project)
	if err != nil {
		return a.fatal(fmt.Errorf("%s", i18n.T("mod.project_failed", project, err)))
	}
	projectType := meta.ProjectType
	if f.str("type", "") != "" {
		projectType = f.str("type", "")
	}
	if strings.EqualFold(projectType, "modpack") {
		// A pack is not a file that belongs in a subdirectory: it is a list of
		// files, a loader and a game version. That is a different install.
		return a.installPack(inst, project, meta, f.str("pack-version", ""), f.boolean("dry-run"), false)
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

// instanceSpec is what an instance turns out to be running, once its version
// JSON has been read.
type instanceSpec struct {
	Game    string
	Loader  string
	Version string // the loader's version, when the profile says
}

// instanceSpecOf works out which game version the instance runs and which loader
// it has, unless the caller already said. It asks the version JSON rather than the
// instance's name: an instance another launcher built (PCL writes `clientVersion`
// and its own id, not `inheritsFrom`) has to answer the same questions as one
// mcli created.
func (a *App) instanceSpecOf(inst *store.Instance, game, loader string) (instanceSpec, error) {
	spec := instanceSpec{Game: game, Loader: loader}
	if spec.Game != "" && spec.Loader != "" {
		return spec, nil
	}
	v, err := a.Loader(true).Lookup(inst.Version, "")
	if err != nil {
		if spec.Game == "" {
			return spec, err
		}
		return spec, nil
	}
	if spec.Game == "" {
		spec.Game = v.GameVersion()
	}
	loaderName, loaderVersion := v.Loader()
	if spec.Loader == "" {
		spec.Loader = loaderName
	}
	spec.Version = loaderVersion
	return spec, nil
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

// packInstall is one resolved pack: which version, what it wants on disk, and the
// file already fetched so its index could be read.
type packInstall struct {
	project  string
	meta     modrinth.Project
	version  modrinth.Version
	packPath string
	cleanup  func()
	idx      mrpack.Index
	items    []dl.Item
	total    int64
	removed  []string
}

// resolvePack picks the pack version to install and reads its index. It is the
// part both installing a pack and updating one need; everything that changes the
// instance happens afterwards, so a dry run can stop right here.
func (a *App) resolvePack(inst *store.Instance, project, selector string, dryRun, updating bool) (*packInstall, int, bool) {
	spec, err := a.instanceSpecOf(inst, "", "")
	if err != nil {
		return nil, a.fatal(err), false
	}
	// Ask for a pack version that fits this instance, not merely the newest one:
	// a pack that still publishes a build for the instance's game version should
	// install that build rather than refuse.
	versions, err := modrinth.Versions(a.Policy(), project, spec.Game, "")
	if err != nil {
		return nil, a.fatal(err), false
	}
	if len(versions) == 0 && spec.Game != "" && selector == "" {
		// Nothing fits: ask again without the filter so the refusal can say what
		// the pack is actually for instead of only "no version found".
		if all, err := modrinth.Versions(a.Policy(), project, "", ""); err == nil && len(all) > 0 {
			versions = all
		}
	}
	if len(versions) == 0 {
		return nil, a.fatal(fmt.Errorf("%s", i18n.T("mod.no_version", project, spec.Game, ""))), false
	}
	chosen, ok := pickPackVersion(versions, selector)
	if !ok {
		return nil, a.fatal(fmt.Errorf("%s", i18n.T("upd.pack_no_version", project, selector))), false
	}
	file, ok := modrinth.PickFile(chosen)
	if !ok {
		return nil, a.fatal(fmt.Errorf("%s", i18n.T("mod.no_file", chosen.Name))), false
	}

	// The index lives inside the .mrpack, so the pack has to be fetched first —
	// even for a dry run, which says so rather than downloading in silence.
	if dryRun {
		a.Log.Printf("%s", i18n.T("mod.pack_dry_fetch", chosen.Name, humanBytes(file.Size)))
	}
	packPath, cleanup, err := a.fetchTemp(file)
	if err != nil {
		if !dryRun {
			return nil, a.fatal(err), false
		}
		a.Log.Printf("%s", i18n.T("mod.pack_dry", chosen.Name, chosen.VersionNumber, humanBytes(file.Size)))
		a.Log.Printf("%s", i18n.T("mcv.dry_run"))
		return nil, 0, false
	}
	idx, err := mrpack.Open(packPath)
	if err != nil {
		cleanup()
		return nil, a.fatal(fmt.Errorf("%s", i18n.T("mod.pack_broken", chosen.Name, err))), false
	}
	job := &packInstall{project: project, version: chosen, packPath: packPath, cleanup: cleanup, idx: idx}

	loader, loaderVersion := idx.Loader()
	game := idx.GameVersion()
	a.Log.Printf("%s", i18n.T("mod.pack_found", idx.Name, chosen.VersionNumber, game))

	// --- does this instance match the pack? ------------------------------
	if game != "" && spec.Game != "" && game != spec.Game {
		if updating {
			return nil, a.fatal(fmt.Errorf("%s", i18n.T("upd.pack_new_game", idx.Name, game, inst.Name, game, inst.Name))), false
		}
		return nil, a.fatal(fmt.Errorf("%s", i18n.T("mod.pack_mismatch", idx.Name, game, inst.Name, spec.Game, game, game))), false
	}
	if loader != "" && loader != "fabric" {
		return nil, a.fatal(fmt.Errorf("%s", i18n.T("mod.pack_loader_unsupported", idx.Name, loader))), false
	}
	if loader == "fabric" && spec.Loader != "" && loaderVersion != "" && loaderDiffers(spec.Version, loaderVersion) {
		// A pack is tested against one loader build. A different one usually runs
		// it, so this is a warning, not a refusal — but staying quiet about it
		// would leave a person blaming the pack when something odd happens.
		have := spec.Version
		if have == "" {
			have = inst.Version
		}
		a.Log.Printf("%s", i18n.T("mod.pack_loader_diff", idx.Name, loaderVersion, inst.Name, have))
	}
	if loader == "fabric" && spec.Loader == "" {
		a.Log.Printf("%s", i18n.T("mod.pack_needs_loader", idx.Name, loaderVersion))
		if !dryRun {
			id, err := a.ensureLoaderTarget(versionRef{id: inst.Version, game: game}, loaderFabric, loaderVersion)
			if err != nil {
				return nil, a.fatal(err), false
			}
			if id != "" {
				inst.Version = id
				if err := a.Store.Save(); err != nil {
					return nil, a.fatal(err), false
				}
			}
		}
	}

	// --- the pack's files ------------------------------------------------
	items, err := mrpack.Items(idx, a.instanceDir(inst.Name))
	if err != nil {
		return nil, a.fatal(fmt.Errorf("%s", i18n.T("mod.pack_broken", idx.Name, err))), false
	}
	job.items = items
	job.total = mrpack.TotalBytes(items)
	if updating {
		job.removed = mrpack.Removed(inst.PackFiles, idx)
	}
	return job, 0, true
}

// applyPack installs a resolved pack: its files, the overrides that ship with it,
// and the record of what it put there, so a later update can take it back.
func (a *App) applyPack(inst *store.Instance, job *packInstall, dryRun, updating bool) int {
	a.Log.Printf("%s", i18n.T("mod.pack_files", len(job.items), humanBytes(job.total)))
	if len(job.removed) > 0 {
		a.Log.Printf("%s", i18n.T("upd.pack_removed", len(job.removed)))
		for _, p := range job.removed[:min(len(job.removed), 10)] {
			a.Log.Infof("%s", p)
		}
		if len(job.removed) > 10 {
			a.Log.Infof("%s", i18n.T("dl.and_more", len(job.removed)-10))
		}
	}
	if dryRun {
		for _, it := range job.items[:min(len(job.items), 10)] {
			a.Log.Infof("%s", it.Dest)
		}
		if len(job.items) > 10 {
			a.Log.Infof("%s", i18n.T("dl.and_more", len(job.items)-10))
		}
		a.Log.Printf("%s", i18n.T("mcv.dry_run"))
		return 0
	}
	if !a.Yeseverything {
		ok, err := a.confirmBatch(job.idx.Name, len(job.items), job.total)
		if err != nil {
			return a.fatal(err)
		}
		if !ok {
			return a.fatal(fmt.Errorf("%s", i18n.T("dl.declined", job.idx.Name)))
		}
	}
	instanceDir := a.instanceDir(inst.Name)
	if err := a.dropPackFiles(instanceDir, job.removed, inst.PackFiles); err != nil {
		return a.fatal(err)
	}
	if err := dl.FetchAll(a.Policy(), job.items, 4, a.Log); err != nil {
		return a.fatal(err)
	}
	written, kept, err := mrpack.ExtractOverrides(job.packPath, instanceDir)
	if err != nil {
		return a.fatal(fmt.Errorf("%s", i18n.T("mod.pack_broken", job.idx.Name, err)))
	}
	if updating {
		a.Log.Printf("%s", i18n.T("upd.pack_moved", inst.Name, inst.PackNumber, job.version.VersionNumber))
	}
	a.Log.Printf("%s", i18n.T("mod.pack_done", job.idx.Name, len(job.items), humanBytes(job.total)))
	a.Log.Printf("%s", i18n.T("mod.pack_overrides", len(written), len(kept)))

	inst.Pack = job.project
	inst.PackVersion = job.version.ID
	inst.PackNumber = job.version.VersionNumber
	inst.PackFiles = mrpack.Digests(job.idx)
	return 0
}

// installPack is `mcli add <pack>`: resolve, then apply.
func (a *App) installPack(inst *store.Instance, project string, meta modrinth.Project, selector string, dryRun, updating bool) int {
	job, exit, ok := a.resolvePack(inst, project, selector, dryRun, updating)
	if !ok {
		return exit
	}
	defer job.cleanup()
	job.meta = meta
	code := a.applyPack(inst, job, dryRun, updating)
	if code != 0 || dryRun {
		return code
	}
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	return 0
}

// dropPackFiles deletes the files an older pack installed that the new one does not
// want. Two things keep it from touching anything else: only paths the old pack
// recorded are considered at all, and a file is deleted only while it still matches
// the digest that pack declared — a file that changed since is someone's own, and
// it is kept and reported instead.
func (a *App) dropPackFiles(instanceDir string, paths []string, recorded map[string]string) error {
	for _, p := range paths {
		dest, err := mrpack.Destination(instanceDir, p)
		if err != nil {
			// A path the pack should not have written is not ours to delete.
			a.Log.Warnf("%s", i18n.T("upd.pack_kept", p))
			continue
		}
		info, err := os.Stat(dest)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.IsDir() {
			continue
		}
		want, known := recorded[p]
		if !known {
			a.Log.Printf("%s", i18n.T("upd.pack_kept_unrecorded", p))
			continue
		}
		got, err := sha1File(dest)
		if err != nil {
			a.Log.Printf("%s", i18n.T("upd.pack_kept_unreadable", p, err))
			continue
		}
		if want != "" && got != want {
			a.Log.Printf("%s", i18n.T("upd.pack_kept_modified", p))
			continue
		}
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// sha1File is the digest of a file on disk: the comparison that decides whether a
// pack's file is still the pack's file.
func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// pickPackVersion resolves what "the pack version to install" means: the newest
// matching one by default, or a specific version when the caller names one by id,
// name or version number.
func pickPackVersion(versions []modrinth.Version, selector string) (modrinth.Version, bool) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return versions[0], true
	}
	for _, v := range versions {
		if v.ID == selector || v.VersionNumber == selector || v.Name == selector {
			return v, true
		}
	}
	return modrinth.Version{}, false
}

// fetchTemp downloads one file to a temporary path and returns it with its
// cleanup. The .mrpack itself is not part of the instance: keeping it would leave
// a stray zip in the game directory.
func (a *App) fetchTemp(file modrinth.File) (string, func(), error) {
	tmp, err := os.CreateTemp("", "mcli-pack-*.mrpack")
	if err != nil {
		return "", func() {}, err
	}
	path := tmp.Name()
	tmp.Close()
	item := dl.Item{
		Candidates: []mirror.Candidate{{URL: file.URL, Source: "modrinth"}},
		Dest:       path,
		SHA1:       file.Hashes.SHA1,
		Size:       file.Size,
		Desc:       filepath.Base(file.Filename),
	}
	if err := dl.FetchAll(a.Policy(), []dl.Item{item}, 1, a.Log); err != nil {
		os.Remove(path)
		return "", func() {}, err
	}
	return path, func() { os.Remove(path) }, nil
}
