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
| 实例 | 多实例隔离，每个实例独立存档与设置；libraries / assets / versions 全局共享 |
| 界面 | 中英文双语，`--lang auto\|en\|zh` |
| 日志 | 终端只显示要点，完整日志写入 `~/.mcl-cli/logs/` |

按需求文档刻意不做：Fabric/Forge/Quilt、Mod 与资源包管理、离线账号、GUI。

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
mcli login                                   # 打印设备码：打开网址、输入代码
mcli list --available --limit 10             # 看能装哪些版本
mcli create vanilla --version 1.21.4         # 建一个隔离实例
mcli launch vanilla                          # 缺什么自动下载，然后启动游戏
```

日常用法：

```sh
mcli launch vanilla --xmx 4G --width 1920 --height 1080 --dry-run   # 只看命令行
mcli launch --last                            # 复用上次的启动参数
mcli list                                     # 列出实例
mcli config set log_level_terminal debug      # 改配置
mcli java which 1.21.4                        # 这个版本需要/将使用哪个 Java
```

## 命令

| 命令 | 作用 |
|---|---|
| `mcli login [--label <名称>] [--timeout <秒>]` | 微软设备码登录 |
| `mcli logout [<账号>] [--all]` | 清除已保存凭证 |
| `mcli list [--instances] [--accounts] [--available] [--type <类型>] [--search <文本>] [--limit N]` | 列出实例、账号或可下载版本 |
| `mcli create <名称> --version <版本> [--java <路径>] [--xmx 4G] [--xms 1G] [--jvm-arg ...] [--game-arg ...]` | 创建实例 |
| `mcli delete <名称> [--keep-files] [--yes]` | 删除实例（连同存档） |
| `mcli launch <名称>\|--last [--version <版本>] [--java <路径>] [--xmx] [--xms] [--jvm-arg] [--game-arg] [--width N] [--height N] [--dry-run] [--wait] [--account <账号>]` | 按需安装并启动 |
| `mcli refresh [--versions] [--all]` | 强制刷新已缓存元数据 |
| `mcli config [get\|set\|unset\|list\|path]` | 读写设置 |
| `mcli java [list \| install <major> \| which <版本>]` | 查看或安装 Java |
| `mcli version`、`mcli help [命令]` | 版本与帮助 |

全局选项可写在任意位置：`--lang <auto\|en\|zh>`、`--config <路径>`、`--yes`
（不再询问）、`--quiet`、`--verbose`、`--no-color`。

## 配置项

`mcli config list` 会带中文说明列出全部；数据存在 `~/.mcl-cli/mcli.db`
（JSON，权限 0600）。

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
| `language` | auto | `auto\|en\|zh` |

环境变量：`MCLI_HOME`（默认 `~/.mcl-cli`）、`MCLI_MINECRAFT`（默认
`~/.minecraft`）、`MCLI_LANG`，以及常见代理变量（`HTTPS_PROXY` /
`ALL_PROXY`）——镜像的跳转节点慢的时候很有用。

## 目录结构

```
~/.mcl-cli/                 与其它启动器不共享的状态
├─ mcli.db                  配置表 + 账号 + 实例 + 版本清单缓存
├─ jdk/<component>/         mcli 安装的 Java 运行时
├─ cache/                   元数据缓存
└─ logs/                    mcli-YYYYMMDD.log、game-<实例>-<时间>.log

~/.minecraft/               与官方启动器完全一致的布局
├─ libraries/               全局共享（maven 结构）
├─ assets/                  全局共享：objects/、indexes/、log_configs/
└─ versions/{版本}/          版本 json/jar 与 natives/
   └─ versions/{实例}/        实例自己的游戏目录（saves、options.txt 等）
```

`mcli.db` 用单个 JSON 文档而不是 SQLite：需求只要一个小配置表，JSON 能让启动器
保持零依赖，理由写在 `docs/design-decisions.md`。

## 账号与安全

只支持微软账号。`mcli login` 只打印设备码和网址，密码不会经过 mcli。
refresh token 与 Minecraft 会话令牌用 AES-256-GCM 加密，密钥由本机标识 +
`mcli.db` 中的随机盐经 PBKDF2-SHA256（20 万次）派生——不依赖系统密钥链，
把 `mcli.db` 拷到别的机器也解不开。

## 镜像行为

每个下载都是一个有序候选列表：BMCLAPI 在前（若已配置），官方地址兜底。
某个候选失败、卡住或 SHA-1 不符就换下一个；整轮失败后按
`download_retry_times` 重试。已实测的映射：

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