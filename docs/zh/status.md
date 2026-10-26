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
* 服务端也进了 `doctor` 与 `repair`。`doctor <服务端>` 会检查：jar 与版本发布的校验和是否一致、
  `eula.txt`、`server.properties`（以及是否与另一个 mcli 服务端配置了同一端口）、该版本需要的 Java、
  记录里那个已经不在运行的 pid、以及最近一次日志的结尾。逐条实测过：jar 被移走时会报出缺失与期望大小
  并以退出码 1 结束；EULA 未被接受时会带上网址报出来；`repair --yes` 不会把 `eula.txt` 改成 true
  （它只拥有 jar）；第二台同端口的服务器会被点名；被 `-9` 杀掉的后台服务器会以「过期 pid」出现；
  `doctor --all` 同时覆盖服务端与实例。
* `repair <服务端>` 故意只取一个文件：`eula.txt`、`server.properties` 和存档都是用户自己的。一个会
  覆写它们、把人存档搞没的「修复」不叫修复。
* 操作手册（skill）按编写规范重写了一遍：原来是 429 行不断追加的教训，现在拆成 150 行的指南
  加上 `references/verification-recipes.md` 与 `references/service-facts.md`。它此前已经漂成
  一份日志（带日期的「已验证 …」记录）、这一页内容的第二份抄本，而且还自相矛盾 —— 其中一行仍写着
  用 SIGINT 停止服务器「存档会在退出时保存」，而上面的实测已经证明信号什么都不保存。现在规则留在
  skill 里，事实与状态留在这一页。
* 全新的 `pnpm install` 会以 `ERR_PNPM_IGNORED_BUILDS`（`Ignored build scripts:
  esbuild@0.21.5`）停下，也就是说文档里写的「怎么构建文档站」在一个干净克隆里跑不通。原因：设置之前
  放在 package.json 的 `pnpm` 字段里，而 pnpm 10 以后直接忽略它（它自己会说「The "pnpm" field in
  package.json is no longer read by pnpm」）；pnpm 12.14 认的名字是 `pnpm-workspace.yaml` 里的
  `allowBuilds`，而不是更早的 `onlyBuiltDependencies`（后者会被读进配置，但不生效）。这是跑
  `pnpm approve-builds --all` 并看它写了什么才确定的。验证方式：删掉 `node_modules` 重装（干净、
  无报错）并构建站点（`pnpm docs:build`，13 页）。
* 控制台闸门必须知道自己在等**哪一次运行**：`latest.log` 在新服务器轮转日志之前，仍然是上一次
  服务器的输出，于是「日志里有没有 `Done (`」这种检查会立刻答是，管道里的命令照样被过早送出 ——
  同样的两条报错，来自一个看上去没问题的闸门。现在要求日志的写入时间在这次启动之后
  （`WaitReady(dir, since, timeout)`），并有测试钉住「过期的 Done 不算就绪」。
* 为残留的控制台命令写过一版 drain，实测之后删掉了：FIFO 缓冲区里的字节在最后一个句柄关闭时就
  被丢弃，所以停掉的服务器管道里留下的东西不会传给下一个服务器。没有什么可清空的，代码也就不留
  —— `internal/mcserver` 里没有 `DrainConsole`。
* 在服务器的命令分发器就绪之前送进去的命令，会被回答成
  `An unexpected error occurred while trying to execute that command` —— 出现了两次，
  都是用脚本往刚启动的服务器里喂 `list` 与 `stop` 时。现在控制台输入泵会先等服务器自己打印的就绪行
  （`Done (`），在此之前不转发任何东西：这段时间里人敲的内容留在终端缓冲里，管道里的内容留在管道里。
  脚本化控制台（`printf 'list\nstop\n' | mcli server start <名称>`）是需要保持可用的场景。
* 控制台管道不能在「另一端还有人拿着」之前打开：以只写方式打开 FIFO 会等待读者，于是
  `mcli server start` 在服务器还没起来时就卡住了。这是靠观察真实运行、而不是靠猜找到的 ——
  mcli 当时正睡在 `fifo_open` 里。现在控制台写入端以读写方式打开（Linux 上立即返回），
  前台输入泵改从子进程的 pid 回调里启动，也就是服务器已经握住管道之后。
  `internal/mcserver/console_unix_test.go` 把两条性质都钉住了：没有读者时打开写入端不阻塞、
  管道只创建一次。
* **对无头服务器来说，信号并不是优雅停止 —— 而 mcli 之前是这么声称的。** `server stop` 一边打印
  「它会先保存存档再退出」，一边发 SIGINT。在本机对着真实的 26.3 服务器实测：SIGINT 与 SIGTERM
  都会让进程直接结束 —— 它自己的日志里既没有 `Stopping server` 也没有 `Saving worlds`，也就是
  存档只保留到最后一次自动保存。真正管用的是服务器自己的控制台命令 `stop`（实测：写入之后日志里
  出现 `Stopping server` 与 `Saving worlds`，监听关闭、pid 清空、端口释放）。现在 `server stop`
  就是把这条命令写进控制台 FIFO；`--force` 会直接结束，并说明它做的是强制结束。
* 前台服务器的 pid 之前只在它**退出之后**才记录，于是 `timeout 100 mcli server start` 会留下一个
  占着端口、而状态里毫无痕迹的服务器 —— `server list` 看不到，`server stop` 也够不着。这个问题正是
  这么被发现的。现在 pid 在进程一出现时就记录（两种模式都一样），进程消失时清空。
* 前台运行现在和后台一样接管控制台（输入走 FIFO、输出到终端、服务器在自己的进程组里），于是
  Ctrl+C、`timeout` 的 SIGTERM、以及 `mcli server cmd <名称> stop` 都会通过它自己的命令停止，
  而不是杀掉一个什么都不会保存的 JVM。
* 一次平台梳理 —— 维护者是在 Windows 的 Git Bash 里跑 mcli 的，只在开发机上能用的启动器算不上启动器。
  发现并修掉的东西，以及现在靠什么检查：
  * **所有 32 位目标都编译不过** —— 微软返回的 XSTS 错误码（2148916233 等）放不进 32 位 int。
    现在它们是具名的 int64 常量，`linux/386`、`windows/386`、`linux/arm` 都能编译。
  * 自动计算堆大小只在 Linux 上生效（读 `/proc/meminfo`）。现在 Windows 问 kernel32
    （`GlobalMemoryStatusEx`），macOS 问 sysctl（`hw.memsize`，可用量取物理内存一半 —— 因为
    macOS 的「空闲内存」含义不同）。解析单独成函数，并有测试覆盖没有 MemAvailable 的老内核和垃圾输入。
  * `doctor` 的可用空间一行在 Windows 上被省略；现在它问 kernel32（`GetDiskFreeSpaceExW`）。
  * classpath 分隔符变成可注入的（`launch.Options.PathSeparator`），于是 Windows 的形状
    —— 用 `;` 连接、带空格的路径要加引号 —— 可以在 Linux 上测。
  * `server attach`/`cmd` 与优雅 `server stop` 在 Windows 上本来就诚实拒绝并给出替代方案；
    现在这写进了文档，而不是留给用户自己踩。
* `scripts/check-cross.sh` 一次性构建全部八个声称支持的目标（linux amd64/arm64/386、
  windows amd64/arm64、darwin amd64/arm64、freebsd amd64）。能编译不等于行为正确 ——
  文档里把两者分得很清楚。
* 整合包更新：`mcli update <实例> --pack`（`add` 与 `update` 都有 `--pack-version`）。用真实整合包
  验证：装上 beta.1，再更新到 beta.2 —— 收回 7 个文件、新增 6 个、变更 1 个。收回清单与我从两份
  已发布索引独立算出的差异完全一致（同样是那 7 条路径）；更新前被我手动替换掉的那个文件连同内容
  一起保留，而整合包自己的 6 个被删除。之所以能这么准，靠的是记录：每个整合包文件都连同它声明的
  摘要一起存下来，只有摘要仍然一致的才会被删。第一版用的是修改时间比较，结果多保留了 3 个本该删掉
  的文件 —— 这也是最后改成按摘要判断的原因。
* `add <整合包>` 的 `--pack-version` 可以指定某个整合包版本；优先选择适配实例游戏版本的那个版本，
  而不是单纯最新的；实例已经在最新的适配版本上时会直接说明。
* TOML 写入器的两个 bug，都是靠「写下去的状态必须能读回来」发现的：
  * 嵌套在结构体里的普通值 map 会被**整个丢掉** —— 不报错、没有键，数据就是不见了。这正好是记录
    整合包文件用的形状，现在它会写成独立的表（`[instances."x".pack_files]`），并有
    `internal/toml/nested_map_test.go` 钉住往返。
  * `omitempty` 之前被忽略，所有可选字段都会写成 `""`、`[]` 或零值：每个实例里都有 `java = ""`、
    `xmx = ""`…… 现在空的可选值不再写出，也就是这个标签一直声称的行为。
* 一个需要知道的后果：state.toml 只有一种形态、没有迁移（既定决策），所以更早的构建写出的文件
  （pack_files 是数组而不是表）会让 mcli 直接停下并报
  `instances.<名称>.pack_files: unsupported field kind map`，而不是去猜。报错会点名字段。
* `instance show` 多了一行：实例是由哪个整合包建起来的。
* `mcli update` 把实例换到另一个游戏版本，三种情况都实测过：
  `update modtest --version 1.21.4` 取回 189 个文件 / 97.7 MiB（这台机器上原本根本没有这个
  版本 —— 客户端 jar、库、natives、资源）并把实例指过去；`update legacy --version 26.3` 把
  一个 1.8.9 的实例移走，并报告 `1.8.9 → 26.3`；一个 Fabric 实例更新到 26.3 时，是为新游戏
  版本构建加载器 profile，而不是丢掉加载器。不带 `--version` 的 `update` 表示最新正式版；
  已经是最新的实例只做完整性检查（`26.3 已经装好且完整`），而它离开的那个版本保留自己的文件，
  因为很可能还有别的实例在跑。
* `instance set --loader vanilla` 对别的启动器写出的 profile 曾经回答「未给出任何选项」：
  它靠 id 的拼写来判断实例有没有加载器。现在它去问版本 JSON —— 和上面整合包那个 bug 是同一
  个修法 —— 而「给了选项但没有需要修改的内容」也单独成句，不再打印整段用法。
* 整合包安装，用真实的整合包验证过：把 `add fabulously-optimized` 装进一个**别的启动器建的**实例，
  工具取回 `.mrpack`（173,859 字节），读出索引（48 个文件、41.9 MB、`fabric-loader 0.19.5`
  + `minecraft 26.3`）并完成安装 —— 45 个 jar 进 `mods/`、3 个 zip 进 `resourcepacks/`、
  51 个文件来自整合包的 `overrides/`（再跑一次写入 0、保留 51，手改过的文件也原样保留），
  每个下载都按整合包给出的 SHA-1 校验。整合包的游戏版本会与实例比对 —— 同一条命令对着 1.21.4 的实例
  会被拒绝并点名，同时打印出应该执行的 `mcli create` 命令 —— 而整合包没有指定的加载器构建只是
  警告，不算失败。会落到实例目录之外的整合包路径一律拒绝，`overrides/` 也绝不覆盖已存在的文件。
* 两个只有真实整合包才会暴露的 bug，都出在「实例到底是什么」这件事上：
  1. 那个实例是 PCL 建的：它写自己的 id，游戏版本放在 `clientVersion` 而不是 `inheritsFrom`。
     从 profile id 上读游戏版本，就会拿整合包的 `26.3` 去比 `26.3-Fabric 0.20.1`，把一个明明
     适配的整合包拒掉。现在版本自己回答这些问题：`Version.GameVersion()` 优先 `inheritsFrom`，
     其次 `clientVersion`，最后才是 id；`Version.Loader()` 从主类和加载器库读出加载器及其版本
     —— 非 mcli 写出的 profile 也能给出同样的答案，加载器版本警告也会打印 `0.19.5` 对 `0.20.1`，
     而不是两个 profile 名字。
  2. zip 会把目录本身也存成条目（`overrides/` 后面什么都没有）。解析这个空路径会让整个覆盖
     文件步骤失败 —— 而那已经是下载完 42 MB 之后。现在会在解析路径之前跳过目录条目；
     `internal/mrpack` 的测试夹具现在也包含这种条目，这正是单测之前漏掉它的原因。
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
* Windows 与 macOS 的实际行为：两者都能编译（`scripts/check-cross.sh`），能在 Linux 上测的
  平台形状部分也都测了（classpath 分隔符、带空格路径的引号、内存解析）。**没有**验证的是在
  Windows 本身上运行 mcli —— 真实启动、后台服务端的控制台那条路径、以及进程运行时的 Windows
  路径处理。
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

1. **CurseForge**：Modrinth 不需要 key，已经接上；CurseForge 每个用户都要 API key，因此
   需要一个配置项和一条「缺 key」的报错路径。尚未开始。
2. **CurseForge 整合包**：`.mrpack` 已经能装也能更新；以 CurseForge zip 形式发布的整合包还没做。
   游戏版本与实例内整合包的更新都已完成（`mcli update`、`mcli update --pack`）。


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
