# 架构

## 分层

```
CLI (main.go, flags.go, commands.go, instance_cmd.go, instance_import.go,
     account_cmd.go, launch_cmd.go, app.go, table.go)
 │  argv parsing, i18n setup, prompting, process spawn
 ├─ internal/mcv      version metadata and file installation
 ├─ internal/java     runtime discovery / installation
 ├─ internal/launch   command line assembly
 ├─ internal/auth     Microsoft → Xbox Live → Minecraft
 ├─ internal/dl       transport: candidate lists, retries, checksums, progress,
 │                    source health
 ├─ internal/mirror   URL mapping (BMCLAPI ↔ official)
 ├─ internal/store    the TOML documents (config, state, credentials, last)
 ├─ internal/config   config table schema + validation
 ├─ internal/secret   token encryption
 ├─ internal/paths    directory layout
 └─ internal/i18n     bilingual string catalogs
```

依赖只会向下：`mcv` 使用 `dl`/`mirror`/`i18n`，从不使用 CLI 层的包。`i18n` 不依赖任何东西。

## 启动流水线

```
mcli launch <instance>
  1. resolve instance → version id → per-instance game dir
  2. mcv.ManifestStore.Get(force=false)
        cached in state.toml (manifest_cache), refreshed when older than
        manifest_ttl_hours (silently when manifest_auto_refresh is on)
  3. mcv.Loader.Lookup(versionID, entry.URL)
        versions/<id>/<id>.json, downloaded if missing, then inheritsFrom
        followed recursively (max depth 5)
  4. Version.Effective()  → merged child/parent document
  5. java.RequiredMajor() → component/major from javaVersion, table as fallback
     java.ScanRuntimes() → Pick() (exact major, else smallest newer)
     no local runtime → offer to install one (java.Installer)
  6. mcv.Installer.Ensure(effective)
        client jar, libraries, natives jars, asset index, asset objects,
        logging config; plan first (count/size), one aggregate Y/N prompt,
        then a 6-worker download pool
  7. auth: stored session, refreshed when it expires within 5 minutes
  8. launch.Prep() → memory flags + JSON jvm/game args + placeholders + classpath
  9. store last launch in last.toml (for --last), spawn java, stream the game's
     stdout/stderr into ~/.mcl-cli/logs/game-<instance>-<timestamp>.log
```

## 实例隔离

* 版本文件位于 `versions/<version-id>/`，由使用该版本的每个实例共享。
* 游戏数据（存档、options.txt、resourcepacks）位于 `versions/<instance>/`，作为 `--gameDir` 传给游戏。
* `libraries/` 和 `assets/` 由所有版本共享，与官方启动器完全一样。

## ~/.mcl-cli 下的五个文档

全部是 TOML，由 `internal/toml` 解析和写入（无依赖），始终原子写入（`.tmp` + rename）：

```
mcli.toml       [config]        ← the config table (see internal/config)
                hand-editable; `config set` patches one line in place, so
                comments, ordering and extra keys survive; no secrets, so it is
                portable between machines

state.toml      state_version      machine-owned, 0600, no secrets → portable
                accounts{}      ← keyed by Minecraft UUID: name, xuid, label,
                                  active flag, timestamps (identity only)
                instances{}     ← name → {version, per-instance java/xmx/xms/args}
                manifest_cache{}← which source served the manifest, when, and the
                                  path of the cached body

refresh.toml    tokens{}        ← keyed by account UUID: the long-lived refresh
                (0600)            token plus the salt its key derives from.
                                  Rewritten at login (not on every token refresh)

session.toml    sessions{}      ← keyed by account UUID: the short-lived
                (0600)            Minecraft access token and its expiry

last.toml       the previous launch (instance, version, java, xmx/xms, window
                size, jvm/game args, game dir) — tiny on purpose: `launch --last`
                never has to read any other document

cache/version_manifest_v2.json   the raw manifest body, exactly as downloaded
```

`state_version` 是 1。形态只有一种，也没有迁移路径：mcli 在旧形态上没有用户，所以来自其他任何地方的文件要么就是这个形态，要么根本不被支持。由*更新*的 mcli 写出的文件会保留它的编号，CLI 给出警告，而不是把它读乱。

## 接管已有的 .minecraft

`mcli instance scan` 列出磁盘上每个 `versions/<id>/` 目录，`mcli instance import <id>` 把其中一个注册为实例。什么都不移动：版本文件留在原处；而且由于 mcli 把 `versions/<instance>/` 当作游戏目录，其他启动器排布出的 profile 目录会保留它的存档、设置和 mod。

其他启动器的产物有两点在这里很关键：

* **压平的 profile。** PCL 把带 mod 的 profile 写成单个版本 JSON，把 `arguments`、`libraries`、`downloads.client` 和 `javaVersion` 全部复制进来，且没有 `inheritsFrom`。这种文档可以原样使用。当某个 profile *确实*有继承时，`mcv.ClientJarTarget` 决定它运行哪个 client jar：先看显式的 `jar` 字段，再看该版本自己目录里已经存在的 jar（PCL 和 HMCL 会把原版 jar 复制进 profile 目录），最后才看父版本。
* **Natives 子目录。** 现代 JSON（以及每个压平的 profile）把 JNA、LWJGL 和 Netty 指向 `natives/` 下它们各自的目录，并给 `java.library.path` 单独指定一个目录，因为这些库在运行时从 classpath 解包自己的 native 代码。`mcv.Version.NativesTargets` 读取这些参数、创建这些目录，`nativeDirsFor` 再把每个 natives jar 解包到基础目录以及版本要求的那个族子目录里。

## 诊断：doctor 与 repair

`mcv.Plan` 是「这个版本需要哪些文件」的唯一描述：版本自身的文件（client jar、libraries、asset index、log config）、natives 以及它们参数所指的目录，还有 asset index 的每一个 object。`Ensure`（安装）、`doctor`（报告）和 `repair`（抓取）都读它，因此它们不可能对某个版本需要什么产生分歧。

* `doctor` 使用 dry 模式的 installer 和 loader，所以它从不写入——连一个目录都不建。`--deep` 对 asset object 做哈希，而不是相信它们的 size。
* `repair` 只抓取 `Plan.Check` 报告的内容，然后跑一次 `Ensure` 以确认结果并解包 natives，最后检查运行时。`--force` 会先删掉计划中的文件，好让下载器真的重新抓取它们。
* 缺失的种类彼此分开——不存在、size 不对、hash 不对——并且先比 size 再比 hash，所以被截断的文件会报告为截断，而不是报告为 hash 不匹配。

## 服务器

服务端安装是另一种形态，不是客户端形态的变体：没有账号、没有 assets、没有 classpath，一个目录、一个世界。因此它有自己独立的小包 `internal/mcserver`，而不是在 `internal/launch` 上加一个开关：

* **只需安装一个文件。** 版本 JSON 里的 `downloads.server` 就是整个安装内容。Mojang 的服务端 jar 是自包含的，而且从 1.18 起它是一个 bundler，首次启动时把需要的 libraries 解包到服务端自己的目录里——不需要组装 classpath，不需要 natives，不需要 asset index。
* **Java 来自同一套规则。** `java.RequiredMajor` 读取服务端版本的 `javaVersion.majorVersion`，缺运行时时的安装方式与客户端相同。
* **两个文本文件，只写一次。** `eula.txt` 和 `server.properties` 在缺失时创建，绝不覆盖：世界、端口和难度属于运行服务器的那个人。EULA 会被打印出来并要求确认，绝不默认同意——`--eula`/`--yes` 跳过的是提问，不是那则提示。
* **默认附着在前台。** 服务器的控制台就是它的 stdin 和 stdout，所以单纯的 `server start` 两者都继承；Ctrl+C 于是能到达服务器，它会保存世界并退出。`--detach` 把这些输出重定向到 `<dir>/logs/console.log`（被分离的子进程不能继承 mcli 的 stdout，否则父进程的调用方会一直等一个永不结束的进程），并让子进程在自己的 session 里启动。
* **停止就是发信号。** `server stop` 读取记录的 pid，检查 `/proc/<pid>/cmdline` 里仍然是这个服务器的 jar，发送 SIGINT，然后等进程消失。绝不用 `kill -9`：世界是在退出过程中保存的。
* **一个可以回来的控制台。** 分离的服务器从 FIFO（`<dir>/console.in`，以读写方式打开，所以永不阻塞）读取自己的命令，并把输出写到 `<dir>/logs/console.log`。`server attach` 就是这条管道的两端——它 tail 那个文件并转发输入的每一行——而 `server cmd` 是同样的写入，只是不建立会话。attach 期间按 Ctrl+C 会让服务器继续运行；停掉它是 `server stop` 的职责。在没有 FIFO 的系统上，这个选项会报告自己不可用，而不是假装可用。

## Fabric profile

Fabric 以版本 JSON 的形式发布它的 loader，该 JSON 继承自游戏版本（`meta.fabricmc.net/v2/versions/loader/<game>/<loader>/profile/json`）：一个 id、一个主类（`KnotClient`）、模拟原版主类的那个 jvm 参数，以及一份 library 列表。这正是 `mcv.Version` 所读取的形态，所以 `internal/fabric` 只需抓取那份文档并写成 `versions/<id>/<id>.json`——它之后的一切（启动、安装、doctor、repair）完全不需要 Fabric 专用代码。

有两点值得记住：

* **旧式写法的 libraries。** profile 用 `name`、`url` 和 `sha1` 列出 libraries，没有 `downloads` 块。因此 `Library.ArtifactDownload` 从 name 拼出 maven 路径、从 `url` 得到仓库；没有这一步，loader 和它的 asm/mixin 依赖永远进不了 classpath，游戏会因为找不到主类而启动失败。其他启动器的 profile（PCL、HMCL）用的是同一种写法，所以这不是 Fabric 独有的问题。
* **我们自己不做合并。** profile 继承原版版本，所以 `mcv.Version.Effective` 已经合并了 library 列表和 java 版本；mcli 从不写一份压平的副本，这样只要再安装一个 profile 就能升级 loader。

## 错误处理风格

错误是返回的，不是 panic 的。CLI 在最顶层统一翻译一次（`App.fatal` → stderr/stdout 一行输出，退出码 1；用法问题 → 退出码 2）。`i18n.T` 用在消息被*格式化*的那一刻，这样被包装过的错误在用户的语言里仍然读得通。

## 下载策略

`dl.Policy` 承载全部传输行为：重试次数、超时、「大文件」阈值、确认回调、logger。卡住由 watchdog reader 检测，20 秒没有字节到达就取消请求（BMCLAPI 的重定向节点确实会卡）。校验是 SHA-1 + size；只有校验通过后才写目标文件，所以被中断的下载不会在共享缓存里留下一个坏文件。

一次运行中的所有 policy 共享同一个 `dl.SourceHealth`：同一来源连续失败三次，就被降到候选列表末尾，本次运行余下部分一直如此（绝不移除——只能访问镜像的网络仍然得装到文件）。正是这一点，让一次安装 700 个文件的 Java 运行时不至于为每个文件付一次卡住超时。