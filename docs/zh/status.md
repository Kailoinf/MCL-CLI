# 工作状态 —— mcli

`AGENTS.md` 保存那些很少变化的规则。这个文件是每次会话都在变的那部分：项目现在
在哪一步、哪些是真的实测过的、还剩什么。跟工作改在同一个 commit 里。

## 现在

* 版本 `0.2.0`（`app.go`），磁盘上的 `state_version = 1`。
* `gofmt -l .` 无输出，`go vet ./...` 干净，`go test ./...` 全绿，
  `go build -o mcli .` 全绿。
* 已实现的命令：`version help list create delete launch refresh config
  java login logout account instance doctor repair server fabric search add mods`（`fix` 是
  `repair` 的别名，`srv` 是 `server` 的别名，`fab` 是 `fabric` 的别名）。
* `docs/` 是一个 VitePress 站点（`pnpm install && pnpm docs:build`，产物在
  `docs/.vitepress/dist`）；`skill/SKILL.md` 随仓库一起发布，并且与它所描述的
  功能一起更新。

### 代码今天假定的目录布局

```
~/.mcl-cli/     mcli.toml（配置）· state.toml（身份，不含密钥）
                refresh.toml（长期 token）· session.toml（短期）
                last.toml（上次启动）· jdk/ · cache/ · logs/
~/.minecraft/   libraries/ · assets/ · versions/<version-id>/ · versions/<instance>/
```

## 用真实命令验证过（2026-10-26）

* 在隔离 home 上的实例生命周期：`create`、`instance list/show/set/copy/
  rename`、`scan`、`import`，以及错误路径（未知实例、未知选项、多余的
  位置参数 → exit 2）。
* 一次**真实的 PCL 安装**（一个 Windows 的 `.minecraft`，用 `7za x` 解到
  `/root/.hermes/cache/scratch/mcuser`）：`instance scan` 找到了
  `26.3-Fabric 0.20.1`，`import` 把它注册进来，`launch --dry-run` 产出了完整的
  命令行 —— 55 个 classpath 条目、client jar 排在最后、取自 profile 自己的
  目录，主类是 `net.fabricmc.loader.impl.launch.knot.KnotClient`，
  `natives/{java,jna,lwjgl,netty}` 被引用并由安装器的解压步骤创建，来自
  `maven.fabricmc.net` 的 Fabric 库被透传到它们自己的 host。磁盘上恰好缺一个
  库（`netty-transport-native-epoll`），真实安装会把它取下来。dry run 报告
  "will download 14 files, 10.0 MiB (5202 already present)"。
* 来自版本 JSON 的值是**原样**透传的，包括一个看起来不对的：这个 profile 的
  `-DFabricMcEmu= net.minecraft.client.main.Main ` 在 Fabric 安装器写入的 JSON
  里确实带一个前导空格和一个尾随空格。不要去"清理"参数值 —— 带空格的路径和带
  空格的游戏参数都得活下来，其他每个启动器也都是照原样透传这一条。
* `launch --version <other>` 把 client jar 和 natives 指向*该版本*的目录，而不
  是实例的目录（之前是错的）。
* 运行时安装：`jre-legacy`（8）和 `java-runtime-delta`（21）已在磁盘上，
  `bin/java -version` 两个都能跑。`java install 25`（java-runtime-epsilon，
  ~700 个文件）走镜像很慢；见"环境怪癖"。
* 部分安装的运行时这个 bug 已修好并有实测：一次被中断的安装（没有
  `.mcli-ok` 标记）不再报告 "already installed"。
* `dl.SourceHealth` 单元测试：六个文件，一个死掉的镜像恰好被尝试三次，官方源
  供应全部六个。
* 对着真实 PCL 安装跑 `mcli doctor` / `mcli repair`：`doctor
  "26.3-Fabric 0.20.1"` 报告 5216 个文件中有 5202 个到位，缺 14 个
  （10.0 MiB），asset index 在位（5147 个对象），natives 尚未解出，一个 mod
  jar，目录大小，并以 1 退出。`repair … --yes` 恰好取回那 14 个文件，重新校验
  （5216/5216），解出 24 个 native 文件 —— 包括
  `natives/lwjgl/linux/x64/org/lwjgl/...` 以及该版本 JSON 指向的空
  `java`/`jna`/`netty` 子目录 —— 并检查了运行时；第二次 `doctor` 没发现任何问
  题，以 0 退出。
* `doctor --all` 在什么都没装时报告环境（home、那五个文件、可用空间、java
  运行时、manifest 缓存、最新的游戏日志），列出实例，并且恰好在有问题时以 1
  退出。
* `launch --server host[:port]`（`--fullscreen`、`--demo`）在两代参数上：现代
  profile 从它自己的 JSON 产出 `--quickPlayMultiplayer
  example.com:25565`（feature `is_quick_play_multiplayer`），且没有
  `--quickPlayPath` —— 只有当启动器有 quick play 文件要交接时才会传这一条 ——
  而 1.8.9 则把经典的 `--server play.example.net
  --port 25566 --demo --fullscreen` 追加到它的 `minecraftArguments` 后面。一个
  坏地址（`host:abc`）exit 2，不带这些开关的启动一条都不加。
  `internal/launch/server_test.go` 把这一切钉住。
* 一台真实服务器，从起到停，就在这台机器上（这正是一台无头机仍然能端到端验证
  的东西）：`server create mc-test --version 26.3
  --port 25566 --eula` 打印了 EULA URL，写出 `eula.txt` 和
  `server.properties`；`server start --detach` 通过 BMCLAPI 取回了
  62,294,556 字节的服务器 jar（与 JSON 声明的尺寸完全一致），bundler 把
  `libraries/` 和 `versions/` 解到服务器的目录里，日志到达
  `Done (3.533s)`，端口监听在 `*:25566`，世界被创建。随后 `server list` 显示
  `运行中` 和 pid，`server stop` 对它发 SIGINT 并在 ~1s 内返回，退出时写下了
  `world/level.dat`，第二次 `stop` 报告 `没有在运行`（exit 1）。
* Modrinth 的搜索与安装，对着线上 API 实测过：`search sodium --type mod --version
  1.21.4 --loader fabric` 列出的 5/35 条结果带下载量与作者；`add sodium --instance mf`
  下载了 Fabric 版本（`sodium-fabric-0.6.13+mc1.21.4.jar`，1,306,799 字节），并按 API 给出的
  SHA-1 校验；再跑一次会报告「已安装且完好」；`mods mf` 把它列了出来。资源包也进了正确目录
  （`add faithful-32x --type resourcepack` 选中 `Faithful 32x - 1.21.4.zip`，10.0 MiB，
  落到 `resourcepacks/`），加载器能从实例自身推断（`fabric`），而原版实例会被拒绝，并给出
  应该执行的命令，而不是塞给它一个加载不了的 jar。
* 模组加载器变成了**版本上的一个选项**，而不是第二个位置参数、也不是一条独立命令：
  `mcli create <名称> --version <版本> --loader vanilla|fabric [--loader-version <版本>]`，
  `mcli instance set` 上是同一对参数；`mcli fabric list <游戏版本>` 只负责发现，
  `fabric install` 已删除。这样做的依据就是通行标准 —— 位置参数只留「命令作用的对象」，
  其余全是取值封闭的选项（GNU 长选项；clap 的 ValueEnum；CMCL 把 loader 挂在它的 version
  命令上，Fabric 自家安装器用 `-loader`）—— 非法的取值会以退出码 2 报错并列出可选值。
* 实测过的往返：`create mf --version 1.21.4 --loader fabric` 装好 0.20.1 的 profile，并把实例
  指向 `fabric-loader-0.20.1-1.21.4`（dry-run：69 个 classpath 条目、`KnotClient`、
  `fabric-loader-0.20.1.jar`）；`instance set modtest --loader fabric` 把已有实例切到该 profile，
  `--loader vanilla` 又把它切回 `1.21.4`。真机测试还抓到一个单测抓不到的 bug：空的
  `--loader-version` 并没有被当作「最新稳定版」，profile URL 里的构建号是空的，于是 404 ——
  `isLatest` 现在接受空字符串，并有测试钉住。
* 分离的服务器不能继承 mcli 的 stdout：一个打开的管道会让每个调用者去等一个
  永不结束的进程。它现在在自己的 session 里写
  `logs/console.log`，所以 `start --detach` 在 0.2s 内返回。

* Fabric，对着同一台机器上 PCL 已经装好的 loader：`fabric list 26.3` 报告
  255 个 build，最新稳定版为 0.20.1，
  已装载的 loader 写进 `versions/fabric-loader-0.20.1-26.3/`，而
  `create --version 26.3 --loader fabric` 产出的实例，其 dry-run 命令行里有
  `KnotClient`、原样保留的 `-DFabricMcEmu= net.minecraft.client.main.Main `、
  父版本的 `26.3/26.3.jar` 和 **62** 个 classpath 条目。这份条目列表与 mcli 为
  PCL 自己的 `26.3-Fabric 0.20.1` profile 构建的那份逐字节相同（两条
  classpath 的 `diff` 为空）。
* 一个只有 Fabric 才暴露出来的真 bug：Fabric 的 profile 把它的库列为
  `name` + `url` + `sha1`，**没有** `downloads` 块，而
  `Library.ArtifactDownload` 对任何没有该块的东西都返回 false —— 于是 loader
  永远到不了 classpath（54 个条目而不是 62 个），游戏会死在找不到主类上。这条
  路径现在由 maven 名字和该条目指名的仓库构建；`internal/mcv/library_url_test.go`
  把它钉住，包括一个 natives classifier 依旧不产生 classpath artifact。
* `internal/i18n/callsites_test.go` 统计每个
  `i18n.T("key", …)` 调用点的参数个数，并与该 key 的动词占位符比较。它第一次
  运行就找出了八处坏调用点（`fab.installed`、`launch.auto_create`、……），这些
  在运行时一直在打印 `%!s(MISSING)` —— catalog 测试看不到这些。
* 三种服务器控制台模式全部实测：前台控制台持有 stdin 打开，跑了 `list`
  （日志 `There are 0 of a max of 20 players
  online:`）和 `stop`（`Stopping the server`），以 exit code 0 结束；
  `server attach` 对着分离的服务器投递了一条管道传入的 `list`；以及
  `server cmd <name> say hello from mcli` 在日志里出现为
  `[Server] hello from mcli`。在服务器控制台线程存在之前就关闭的管道会丢掉它
  的命令，所以在日志显示 `Done` 之前要一直保持 stdin 打开。
* 不兼容的决定仍然有效：TOML 之前的 JSON 数据库导入、带内联 token 的 v2
  `state.toml`、`paths.LegacyDB` 以及每一个迁移测试都已删除，而且这个 store 在
  任何地方都不再被叫做 database（`store.Store`、`store.StateVersion`、磁盘上的
  `state_version`）。
* 应维护者要求，第二个更新的版本被端到端取回：
  `create modern --version latest`（26.3，manifest 当前的 release）然后
  `repair modern --yes` 走 BMCLAPI —— 5216/5216 个文件，5147 个 asset 对象，
  natives 已解出，`doctor` 干净。旁边的 Fabric fixture 保持完好。
* `auto_memory_percent` 现在真的在给堆定尺寸：在这台 7.5 GiB 的机器上，25 →
  `-Xmx1043M`，50 → `-Xmx2085M`，75 → `-Xmx3142M`，被夹在物理内存的一半以
  下，并且从不高于空闲内存。
* `internal/mcv/plan_test.go` 离线钉住 plan：文件列表、natives 数量、位于
  `objects/<first2>/<hash>` 的 asset 对象、全部三种缺失类型
  （不存在 / 尺寸不对 / 哈希不对），以及规划本身不写任何东西。

## 没有验证，以及原因

* 真实登录：需要人在浏览器里用一个微软账号操作。绝不要尝试，绝不要索要凭据。
* 真实启动游戏：这台机器是无头的，而且维护者明确要求不要尝试启动。正确性由
  `launch --dry-run` 的输出判断（classpath、占位符、natives 路径、游戏参数）。
* Windows：维护者在 Windows 上从 UCRT64（Git Bash）运行 mcli；上面所有内容都是
  在 Linux 上验证的。只有 Windows 才有的风险：natives 参数里的路径分隔符、对
  含空格的实例名加引号，以及 `minecraft_dir` 里的盘符路径。
* 维护者自己的 `.minecraft` 一个字节都没被写：归档被解到 scratch，测试用的所有
  home 都位于 `/root/.hermes/cache/scratch/` 下（`mtest`、`mtest2`、
  `mcuser-home`）。

## 服务事实（用真实请求验证过，2026-10）

* `bmclapi2.bangbang93.com/mc/game/version_manifest_v2.json` 可能对一个会卡住的
  `*.sakura.sld.tw` 节点返回 302；官方 `piston-meta.mojang.com` 那份才是可靠的
  回退。因此每一次下载都是一份候选列表，`dl` 保留它 20 s 的卡死看门狗 —— 不要
  删掉官方回退。
* BMCLAPI 提供 `/version/<id>/json` 和 `/version/<id>/client`，原样镜像
  `piston-meta`/`piston-data` 的路径，maven 库在 `/maven/<path>`，asset 对象在
  `/assets/<first2>/<hash>`，Forge maven 在 `/maven` 下。**没有**
  `/assets/indexes/<id>.json`（404）—— 用
  `/v1/packages/<sha1>/<id>.json`。`/java/list` 是 403：不要在它上面建东西。
* 其他 host 上的库（`maven.fabricmc.net`、`maven.minecraftforge.net`）原封不动
  透传到它们的官方 URL；只有 Mojang 的 host 被镜像。
* Mojang 的 java runtime product manifest
  （`launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96…/all.json`）由
  BMCLAPI 镜像，先按平台（`linux`、`linux-i386`、`windows-x64`、
  `mac-os`、`mac-os-arm64`、……）再按组件索引。每个组件 manifest 列出 `files`，
  带 `type: file|directory|link`、`executable` 和 `downloads.raw`
  （sha1/size/url 在 piston-data 上）。见过的组件：`jre-legacy`=8、
  `java-runtime-alpha`=16、`-beta`/`-gamma`=17、`-delta`=21、`-epsilon`=25。
* 版本 JSON：现代文档带 `arguments.jvm`/`arguments.game`（一个字符串或
  `{rules, value}`）、`javaVersion.{component,majorVersion}`，natives 则是名为
  `…:natives-linux` 的独立库；老文档带 `minecraftArguments`、一个 `natives`
  map 和 `downloads.classifiers`。两条路都必须继续能用。当前 release 线
  （`26.3`，快照 `26.4-snapshot-5`）需要 **Java 25**；
  `javaVersion.majorVersion` 是权威。
* 登录是两条流程的事，这不是装饰性的：浏览器流程（默认）用
  `login.live.com/oauth20_authorize.srf` 加 `response_type=code`，一个位于
  29116-29126 的 loopback 服务器，以及 `login.live.com/oauth20_token.srf` 做换
  票 —— 没有 client secret，没有 PKCE。设备流程用
  `login.microsoftonline.com/consumers/oauth2/v2.0/devicecode`。参考启动器的
  client id（`288ec5dd-6736-4d4b-9b96-30e083a8cad2`）被 Microsoft 接受，但答
  `AADSTS9002346`（"not configured for the device code flow"），而
  `00000000402b5328` 已经没了（`AADSTS700016`）—— 所以浏览器流程是默认，
  `auth_client_id` 可配置。
* Xbox Live 链路：`user.auth.xboxlive.com/user/authenticate`、
  `xsts.auth.xboxlive.com/xsts/authorize`、
  `api.minecraftservices.com/authentication/login_with_xbox`、
  `/minecraft/profile`。值得翻译成中文提示的 XSTS code：2148916233（没有 Xbox
  profile）、2148916235（区域）、2148916238（儿童账号）。

## 实际跑通过的常用操作

```sh
# 隔离 home —— 永远不要把一次运行指向真实的 ~/.mcl-cli
export MCLI_HOME=/root/.hermes/cache/scratch/mtest2/home
export MCLI_MINECRAFT=/root/.hermes/cache/scratch/mtest2/mc

./mcli version && ./mcli help && ./mcli config path
./mcli list --available --limit 5
./mcli create demo --version 1.21.4 --force        # --force 跳过 manifest
./mcli instance set demo --xmx 4G --jvm-arg=-XX:+UseG1GC
./mcli instance show demo && ./mcli instance list
./mcli instance copy demo clone --with-files && ./mcli instance rename clone renamed
./mcli launch demo --dry-run --yes                 # 检查 java 命令行
./mcli doctor --all                                # 这里哪儿不对，逐个实例看
./mcli repair demo --dry-run                       # repair 会抓取什么（坏掉时 exit 1）
./mcli java which 1.21.4                           # -> 21
```

* 不带账号的浏览器登录（证明 loopback + 换票接线）：在后台启动
  `./mcli login --no-browser --timeout 25`，从打印出的 `redirect_uri` 里读出端
  口，然后
  `curl "http://localhost:<port>/authentication-response?code=FAKE"`。预期看到
  回调页面，以及 Microsoft 对假 code 给出的 `invalid_grant`。
* 真实安装：`./mcli create tiny --version rd-132211 && ./mcli launch tiny --yes`
  会装一个 Java 运行时加上尽可能小的版本，然后在 "no account is logged in" 处
  停下 —— 这是这次运行的预期终点。复用 `MCLI_HOME`，第二次运行瞬间完成。
* 接管一份真实安装：把 `7za x .minecraft.7z` 解到 scratch 目录，把
  `MCLI_MINECRAFT` 指向解开的 `.minecraft`，然后 `instance scan` →
  `instance import "<name>"` → `launch "<name>" --dry-run --yes`。
* 检查 dry-run 命令行要看：classpath 以 client jar 结尾、log4j 参数在 `-cp`
  之前、`-Djava.library.path` 和 natives 子目录、`--gameDir` 指向该实例、
  `--assetIndex` 与该版本的 asset id 一致。

## 值得记住的环境怪癖

* BMCLAPI 会对会卡住的 `*.sakura.sld.tw` 节点返回 302，伴随
  `net/http: TLS handshake timeout`。关于某个候选失败的那些 `WARN` 行是正常
  的。官方回退必须留在每一份候选列表里；除此之外 `dl.SourceHealth` 现在会把连
  续失败三次的源在本轮余下的时间里移到列表末尾（每个源一次，只记一次日志）。
* 一个 Java 运行时~700 个小文件，所以卡住的镜像在那里最伤：第一次尝试
  `java-runtime-epsilon` 时，在下载被中断之前花了好几分钟在 TLS 超时上。尽量
  在网络好的时候跑它，并且重跑以续传（校验会跳过已经正确的部分）。
* 一次完整的现代安装很大：26.3 的 assets ~474 MB，libraries ~88 MB。一台已
  经有另一个启动器数据的机器几乎不需要重新下载这其中的任何东西，这正是
  `instance import` 的意义。
* `7za`（p7zip）解开 `.minecraft.7z`；维护者上传的那份在
  `/root/mcli-test/.minecraft.7z`（压缩后 506 MB，解开后 630 MB）。

## 计划中的工作，按维护者要求的顺序


1. **Mod 和资源包搜索** —— 照 PCL 和 Console Minecraft
   Launcher 的做法：查询 Modrinth 和 CurseForge 的搜索 API、列出结果，并把一个
   命中项装进某个实例的 `mods/` 或 `resourcepacks/`。搜索本身保持单薄（一次查
   询、一页、只取重要的那几个字段）—— 有意思的工作是把结果装进正确的实例。

## 约定（AGENTS.md 故意没写的细节）

* **加一条命令**：usage/description 键（`cli.cmd.<name>.usage`、`.desc`）在两
  份 catalog 里各加一条，在 `printHelp`（main.go）的 `cmds` 表里和 `run` 里的
  `switch` 里各加一条，然后是搭在
  `parseFlags("x", args, map[string]flagSpec{...})` 之上的
  `func (a *App) cmdX(args []string) int`。未知选项保持是错误，这样一个笔误就不
  能改变行为，多余的位置参数用 `cli.unexpected_arg` 拒绝而不是忽略。
  `a.fatal(err)` 退出 1，`a.usageError(...)` 退出 2。
* **表格**按显示宽度补齐（`table.go`），绝不用 `% -20s`：一个 CJK 路径或一个中
  文状态词会让后面每一列都错位。
* **东西放在哪**：`main.go` 分发/帮助 · `commands.go`
  login/logout/list/create/delete/refresh/config/java · `instance_cmd.go`
  instance 子命令 · `instance_import.go` scan/import + 名字规则 ·
  `account_cmd.go` 账号 · `doctor_cmd.go` doctor/repair · `launch_cmd.go`
  launch + java 选择 ·
  `internal/{i18n,paths,toml,config,store,secret,logx,mirror,dl,mcv,java,auth,launch}`。
* **测试保持离线**：`httptest` 代替会失败的镜像，`internal/mcv` 用合成的版本
  JSON 和 zip fixture，`internal/store` 用临时目录。解析和判断被测试；网络、浏
  览器和显示器靠手工验证。

## 未了结的事，按重要性排序

1. **`mcli launch --server host[:port]`**（现代版本要
   `--quickPlayMultiplayer`，老的要 `--server`/`--port`）以及一个 `--fullscreen`
   等价物，给那些直接进服务器的人用。
2. **实例级 `--force` 重装**（`launch --force` 重新下载实例的文件而不碰存档）
   —— `repair --force` 已经覆盖了这件事的文件那一半。
3. **Windows 走一遍**：在维护者的机器（UCRT64）上跑
   scan/import/dry-run/doctor 这条链，并修掉冒出来的任何路径处理问题
   （`disk_windows.go` 只是省掉了可用空间那一行；doctor 其余部分与路径无关）。
4. **`mcli doctor` 管 Java**：doctor 报告某个版本需要哪个运行时以及会用哪个，
   但还不检查被选中的运行时是否真的能起来。
5. **Windows 上的 `server stop`**：那里无法投递控制台中断，所以这条命令把 pid
   报告为不可停止，并指向 `--force`（Kill）。Windows 那一遍应该决定是保留这个，
   还是改成驱动控制台。
6. **`doctor` 里的服务器**：健康检查只覆盖客户端实例；服务器的 jar、世界、端
   口和运行时可以用同一条线。