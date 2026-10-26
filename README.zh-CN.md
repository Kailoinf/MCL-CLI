# mcli — Minecraft 命令行启动器

纯命令行的 **Minecraft：Java 版** 启动器，Go 编写，**零第三方依赖**。仅支持微软
正版账号，没有 GUI，没有交互式向导，一切靠参数驱动。

* 英文文档：[README.md](README.md)
* 架构与决策：[docs/architecture.md](docs/architecture.md)、
  [docs/design-decisions.md](docs/design-decisions.md)
* 给 Agent 的约定：[AGENTS.md](AGENTS.md)

## 功能一览

| | |
|---|---|
| 登录 | 微软设备码流程；refresh token 用 AES-256-GCM 加密后存盘，密钥由本机信息派生 |
| 下载 | 优先 BMCLAPI 镜像，失败自动回退官方源；SHA-1 校验；失败重试；大文件先确认 |
| 版本 | release / snapshot / old_beta / old_alpha；清单带 TTL 缓存；支持 inheritsFrom 合并与老版 minecraftArguments |
| Java | 扫描本机 Java（PATH、JAVA_HOME、发行版目录、官方启动器的 runtime、mcli 自己的 jdk/），按版本所需主版本匹配；找不到可从镜像安装 |
| 实例 | 多实例隔离，每个实例独立存档与设置；libraries / assets / versions 全局共享；随时用 `mcli instance` 修改、复制、改名，也能接管别的启动器装好的 `.minecraft`（instance scan / import） |
| 账号 | 同时支持多个微软账号：`mcli account list` 查看、`use` 切换、`launch --account` 单次指定；刷新令牌与会话令牌分别加密存放在两个文件里 |
| 界面 | 中英文双语，`--lang auto\|en\|zh`；表格按显示宽度对齐，中文路径不会挤歪列 |
| 日志 | 终端只显示要点，完整日志写入 `~/.mcl-cli/logs/` |

按需求文档刻意不做：安装 Fabric/Forge/Quilt、Mod 与资源包管理、离线账号、GUI。
但"运行别人装好的整合包"是支持的：接管后照原样启动，只是不帮你装加载器。

## 安装

```sh
git clone https://github.com/Kailoinf/MCL-CLI.git
cd MCL-CLI
go build -o mcli .        # 需要 Go 1.24+（用了 crypto/pbkdf2 等新标准库）
./mcli version
```

也可以 `install -m755 mcli /usr/local/bin/mcli`。

## 快速上手

```sh
mcli login                                   # 打开浏览器授权（可用 --flow device 走设备码）
mcli account list                            # 看已登录的账号，以及当前默认账号
mcli list --available --limit 10             # 看能装哪些版本
mcli create vanilla --version latest         # 建一个隔离实例（latest | latest-snapshot | 具体版本号）
mcli launch vanilla                          # 缺什么自动下载，然后启动游戏
```

机器上已经有 `.minecraft`（官方启动器 / PCL / HMCL 装的）？直接接管：

```sh
mcli instance scan                           # 列出 versions/ 下的全部版本，含未注册的
mcli instance import "26.3-Fabric 0.20.1"    # 接管其中一个，文件与存档都留在原处
mcli launch "26.3-Fabric 0.20.1"             # 跑现成的整合包；mcli 不负责安装加载器
```

日常用法：

```sh
mcli launch vanilla --xmx 4G --width 1920 --height 1080 --dry-run   # 只看命令行
mcli launch vanilla --xmx 4G --xms 1G        # 本次临时覆盖
mcli launch --last                            # 复用上次的启动参数（直接 mcli launch 也一样）
mcli launch 1.21.4                            # 也可以直接给版本号，会自动建实例
mcli list                                     # 列出实例
mcli instance set vanilla --xmx 6G            # 永久修改实例设置
mcli instance show vanilla                    # 看这个实例实际会用哪些参数
mcli config set log_level_terminal debug      # 改配置
mcli java which 1.21.4                        # 这个版本需要/将使用哪个 Java
```

## 命令

| 命令 | 作用 |
|---|---|
| `mcli login [--flow browser\|device] [--label <名称>] [--no-browser] [--no-switch] [--timeout <秒>]` | 登录微软账号（默认浏览器流程）；--no-switch 只添加账号、不改默认账号 |
| `mcli logout [<账号>] [--all]` | 清除凭证（账号信息与两个令牌文件一起清） |
| `mcli account [list \| use <名称\|UUID\|标签> \| remove <名称>]` | 查看与切换已登录账号 |
| `mcli list [--instances] [--accounts] [--available] [--type <类型>] [--search <文本>] [--limit N]` | 列出实例、账号或可下载版本 |
| `mcli create <名称> --version <版本>|latest|latest-snapshot [--java <路径>] [--xmx 4G] [--xms 1G] [--jvm-arg ...] [--game-arg ...] [--force]` | 创建实例 |
| `mcli instance [list]` | 列出实例：版本、Java、就绪状态、上次启动、游戏目录 |
| `mcli instance show <名称>` | 查看某个实例的全部设置 |
| `mcli instance set <名称> [--version <版本>] [--java <路径>|auto] [--xmx 4G|auto] [--xms 1G|auto] [--jvm-arg ...] [--game-arg ...] [--clear-jvm-args] [--clear-game-args]` | 修改实例设置 |
| `mcli instance copy <源> <目标> [--with-files]` | 复制实例（可连存档一起） |
| `mcli instance rename <旧名> <新名>` | 实例改名，游戏目录一起搬 |
| `mcli instance scan [--all]` | 列出磁盘上的版本目录：类型、JSON/JAR、父版本、是否已注册 |
| `mcli instance import <版本> [--as <名称>] [--force]` | 接管已有安装（官方启动器 / PCL / HMCL / 整合包） |
| `mcli server create <名称> --version <版本> [--java <路径>] [--xmx 2G] [--xms 512M] [--port 25565] [--dir <路径>] [--jvm-arg ...] [--eula]` | 注册服务器：独立目录、存档与端口；会打印 Minecraft EULA 并要求确认 |
| `mcli server [list \| show <名称> \| set <名称> [--xmx] [--xms] [--port] [--java] [--version] \| delete <名称> [--keep-files] [--yes]]` | 查看与修改服务器 |
| `mcli server start <名称> [--dry-run] [--detach] [--eula] [--port N]` | 缺少时下载服务端 jar 并启动；默认前台，控制台直接可用，Ctrl+C 会保存并停止 |
| `mcli server stop <名称> [--force]` | 让后台运行的服务器退出：发送 SIGINT（等同 Ctrl+C），因此存档会被保存 |
| `mcli doctor [<名称>] [--all] [--deep]` | 体检：本机环境（目录、Java、版本清单、日志）＋实例需要的每个文件；有问题时退出码 1 |
| `mcli repair <名称> [--deep] [--force] [--dry-run]` | 只补齐 doctor 查出的缺失/损坏文件（--force 则按清单全部重下） |
| `mcli delete <名称> [--keep-files] [--yes]` | 删除实例（连同存档） |
| `mcli launch <名称>|<版本>|--last [--version <版本>] [--java <路径>] [--xmx] [--xms] [--jvm-arg] [--game-arg] [--width N] [--height N] [--server host[:port]] [--fullscreen] [--demo] [--dry-run] [--wait] [--account <账号>]` | 按需安装并启动 |
| `mcli refresh [--versions] [--all]` | 强制刷新已缓存元数据 |
| `mcli config [get\|set\|unset\|list\|path]` | 读写设置 |
| `mcli java [list \| install <major> [--force] \| which <版本>]` | 查看或安装 Java（--force 重装没装完的运行时） |
| `mcli version`、`mcli help [命令]` | 版本与帮助 |

全局选项可写在任意位置：`--lang <auto\|en\|zh>`、`--config <路径>`、`--yes`
（不再询问）、`--quiet`、`--verbose`、`--no-color`。

## 配置项

`mcli config list` 会带中文说明列出全部；`mcli config path` 打印每个文件的位置。
mcli 在 `~/.mcl-cli/` 里放五个小 TOML 文件：

| 文件 | 内容 | 说明 |
|---|---|---|
| `mcli.toml` | 配置表 | 可手工编辑；`config set` 只改一行，注释与顺序保留；可跨机器使用 |
| `state.toml` | 账号（只有身份信息）、实例、清单缓存指针 | 机器状态（0600），不含任何密钥，可以随便拷 |
| `refresh.toml` | 每个账号的长期刷新令牌及其 salt | 0600；登录时写入，可当密码一样备份 |
| `session.toml` | 短期 MC 会话令牌与过期时间 | 0600；每次刷新都会重写，删掉也没事（会用 refresh.toml 重新换） |
| `last.toml` | 上次启动参数 | 刻意很小，`launch --last` 不必读别的文件 |

| 键 | 默认值 | 说明 |
|---|---|---|
| `download_retry_times` | 3 | 每个文件的尝试次数（含首次） |
| `http_timeout` | 30 | HTTP 超时（秒） |
| `bmclapi_endpoint` | `https://bmclapi2.bangbang93.com` | 镜像根地址 |
| `mirror_fallback` | true | 镜像失败时回退官方源 |
| `manifest_auto_refresh` | true | 清单过期后静默自动刷新 |
| `manifest_ttl_hours` | 6 | 清单缓存有效期 |
| `minecraft_dir` | `~/.minecraft` | 游戏根目录（与官方启动器一致） |
| `versions_dir` | `versions` | 版本/实例目录名 |
| `java_dir` | `~/.mcl-cli/jdk` | mcli 下载的 Java 存放处 |
| `java_path` | 空 | 强制指定 java 可执行文件 |
| `log_level_file` / `log_level_terminal` | debug / info | `debug\|info\|warn\|error\|off` |
| `auto_memory_percent` | 50 | 自动分配堆内存时占可用内存比例 |
| `default_xmx` / `default_xms` | 自动 | 堆内存默认值，如 `4G` |
| `extra_jvm_args` | 空 | 追加到所有启动的 JVM 参数 |
| `confirm_large_download` | true | 大文件下载前 Y/N 确认 |
| `large_download_mb` | 50 | 多大算大文件 |
| `close_after_launch` | true | 启动后立即返回 |
| `auth_client_id` | 参考项目的公开 id | 登录用 OAuth client id |
| `language` | auto | `auto\|en\|zh` |

环境变量：`MCLI_HOME`（默认 `~/.mcl-cli`）、`MCLI_MINECRAFT`（默认
`~/.minecraft`）、`MCLI_LANG`，以及常见代理变量（`HTTPS_PROXY` /
`ALL_PROXY`）——镜像的跳转节点慢的时候很有用。

## 目录结构

```
~/.mcl-cli/                 与其它启动器不共享的状态
├─ mcli.toml                配置表（可手改的 TOML）
├─ state.toml               账号身份、实例、清单指针（0600，无密钥）
├─ refresh.toml             长期刷新令牌（每账号一条，0600）
├─ session.toml             短期会话令牌与过期时间（0600）
├─ last.toml                上次启动参数，供 mcli launch --last
├─ jdk/<component>/         mcli 安装的 Java 运行时
├─ cache/                   缓存的版本清单（原始 JSON）
└─ logs/                    mcli-YYYYMMDD.log、game-<实例>-<时间>.log

~/.minecraft/               与官方启动器完全一致的布局
├─ libraries/               全局共享（maven 结构）
├─ assets/                  全局共享：objects/、indexes/、log_configs/
└─ versions/{版本}/          版本 json/jar 与 natives/
   └─ versions/{实例}/        实例自己的游戏目录（saves、options.txt 等）
```

拆成五个文件是刻意的：TOML 由 `internal/toml` 自己读写（零依赖），文件名与格式
一致，配置本来就是给人改的，上次启动参数单独放一个小文件；最重要的是凭证不跟
"每次都会重写的东西" 待在同一个文件里（理由见 `docs/design-decisions.md`）。

## 账号与安全

只支持微软账号，但可以同时存很多个：再登录一次（可加 `--no-switch` 只添加不改默认），
`mcli account list` 全部列出，`use` 切换默认账号，`mcli launch <实例> --account <名称>`
只对这一次生效。`mcli login` 会打开微软授权页（如果你的 client id 支持设备码，可用
`--flow device` 切换），密码不会经过 mcli。

令牌用 AES-256-GCM 加密，密钥由本机标识 + **每个账号自己的 salt**（与账号一起存放）
经 PBKDF2-SHA256（20 万次）派生：长期刷新令牌在 `refresh.toml`，短期 MC 令牌在
`session.toml`，都是 0600。账号列表与密钥文件分开，所以令牌文件缺失或来自别的机器时
`mcli account list` 照样能用。不依赖系统密钥链，账号条目拷到别的机器也解不开；配置、
实例、上次启动参数都不含密钥，可直接跨机器使用。

## 服务器

服务端和客户端放在一起管理，但它完全是另一套东西：服务器没有账号、没有资源文件、也不需要 classpath，所以 `mcli server` 只下一个文件然后跑起来。

```sh
mcli server create survival --version 26.3 --port 25566 --xmx 4G   # 打印 EULA 网址并要求按 Y
mcli server start survival                                          # 首次约 60MB，之后前台运行
mcli server stop survival                                           # 只有 --detach 启动的才需要手动停
```

* 目录是 `<minecraft_dir>/servers/<名称>`：jar、存档、`eula.txt`、`server.properties` 都在里面，别的操作不会碰它们；已存在的 `server.properties` 永不覆写。
* EULA 不会替你同意：先打印网址，再问一次；`--eula` 或全局 `--yes` 只是跳过提问，不等于不提。
* `--detach` 会后台启动并记录 pid，之后 `mcli server stop` 用它发信号。后台服务器的控制台输出在 `<dir>/logs/console.log`，服务器自己的日志在 `<dir>/logs/latest.log`。
* Java 与客户端同一套规则：用版本 JSON 里的 `javaVersion.majorVersion`，缺了就由 mcli 安装。

## 镜像行为

每个下载都是一个有序候选列表：BMCLAPI 在前（若已配置），官方地址兜底。某个候选失败、
卡住或 SHA-1 不符就换下一个；整轮失败后按 `download_retry_times` 重试。同一个源连续失败
三次会被降到最后（不删除——只能连镜像的网络照样要能装上），并在日志里说明一次。
已实测的映射：

| 官方 | BMCLAPI |
|---|---|
| `piston-meta.mojang.com/<路径>` | `<endpoint>/<路径>` |
| 版本 JSON 地址 | `<endpoint>/version/<版本>/json` |
| `piston-data…/v1/objects/<sha1>/client.jar` | `<endpoint>/version/<版本>/client` |
| `libraries.minecraft.net/<路径>` | `<endpoint>/maven/<路径>` |
| `resources.download.minecraft.net/<两位>/<hash>` | `<endpoint>/assets/<两位>/<hash>` |
| `launchermeta.mojang.com/v1/products/java-runtime/<…>/all.json` | `<endpoint>/v1/products/java-runtime/…` |

## 开发

```sh
go build ./...
go test ./...          # 单测覆盖：规则求值、继承合并、参数拼装、下载重试与
                       # SHA-1 校验、natives 解压、加密、配置、存储、Java 匹配
gofmt -l .             # 必须为空
./mcli --lang en help  # 双语界面可随时切换检查
```

i18n 测试会在以下情况直接失败：代码里出现 `i18n.T("键")` 但词表没有该键，
或者中英词表的键不一致——翻译不会悄悄烂掉。

## 许可证

暂未添加 LICENSE 文件，重新分发前请先问维护者。