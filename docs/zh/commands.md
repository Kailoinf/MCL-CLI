# 命令

每个命令都接受这些全局选项：`--lang <en|zh>`、`--config <path>`、`--yes`（从不询问）、`--quiet`、`--verbose`、`--no-color`、`-h/--help`。

## 账号

| 命令 | 作用 |
| --- | --- |
| `mcli login [--flow browser\|device] [--label <name>] [--no-browser] [--no-switch] [--timeout <sec>]` | 微软账号登录（默认浏览器流程） |
| `mcli logout [<account>] [--all]` | 删除已保存的凭据 |
| `mcli account [list \| use <name> \| remove <name>]` | 查看并切换已登录的账号 |

## 实例

| 命令 | 作用 |
| --- | --- |
| `mcli list [--instances] [--accounts] [--available] [--type <t>] [--search <text>] [--limit N]` | 实例、账号或可下载的版本 |
| `mcli create <名称> --version <版本> [--loader vanilla\|fabric] [--loader-version <版本>] [--java <路径>] [--xmx 4G] [--xms 1G] [--jvm-arg …] [--game-arg …] [--force]` | 创建实例，可选模组加载器 |
| `mcli launch <name>\|<version>\|--last [--version <v>] [--java <path>] [--xmx] [--xms] [--jvm-arg] [--game-arg] [--width N] [--height N] [--server host[:port]] [--fullscreen] [--demo] [--dry-run] [--wait] [--account <a>]` | 按需安装并启动 |
| `mcli instance [list \| show <name> \| set <name> … \| copy <src> <dst> [--with-files] \| rename <old> <new> \| scan [--all] \| import <version> [--as <name>]]` | 查看和修改实例，接管已有的安装 |
| `mcli delete <name> [--keep-files] [--yes]` | 删除一个实例 |
| `mcli update [<实例>] [--version <版本>] [--loader vanilla\|fabric] [--loader-version <版本>] [--pack] [--pack-version <版本>] [--dry-run] [--force]` | 把实例换到另一个游戏版本（默认最新正式版），加 `--pack` 则换它整合包的版本；保留加载器、设置与全部文件 |
| `mcli doctor [<name>] [--all] [--deep]` | 体检本机、每个实例，以及服务端（jar 与版本校验和、EULA、端口、最近日志）；发现问题时以 1 退出 |
| `mcli repair <name> [--deep] [--force] [--dry-run]` | 只抓取缺失或损坏的文件；对服务端只取 jar，绝不动 `eula.txt`、`server.properties` 和存档 |
| `mcli refresh [--versions] [--all]` | 强制刷新缓存的元数据 |

## 服务器

| 命令 | 作用 |
| --- | --- |
| `mcli server create <名称> --version <版本> [--java <路径>] [--xmx 2G] [--xms 512M] [--port 25565] [--dir <路径>] [--jvm-arg …] [--eula]` | 注册一个服务器 |
| `mcli server set <名称> [--version <版本>] [--port N] [--java <路径>] [--xmx] [--xms] [--jvm-arg …] [--clear-jvm-args] [--eula]` | 修改服务器；`--eula` 用于事后接受协议（打印网址并提问） |
| `mcli server [list \| show <name> \| set <name> … \| delete <name> [--keep-files]]` | 查看和修改服务器 |
| `mcli server start <name> [--dry-run] [--detach] [--eula] [--port N]` | 按需抓取 jar 并启动 |
| `mcli server attach <name> [--lines N]` | 跟随后台服务器的控制台 |
| `mcli server cmd <name> <command…>` | 发送一条命令 |
| `mcli server stop <名称> [--force]` | 用真正会保存存档的方式停止：把服务器自己的 `stop` 写进它的控制台；`--force` 则直接结束 |

## Fabric

| 命令 | 作用 |
| --- | --- |
| `mcli fabric list <game> [--limit N]` | 某个游戏版本已发布的加载器构建 |
| `mcli fabric list <版本> [--limit N]` | 列出 Fabric 发布的加载器构建（安装用 `create --loader fabric`） |

## 模组与资源（Modrinth）

| 命令 | 作用 |
| --- | --- |
| `mcli search <关键词> [--type mod\|resourcepack\|shader\|datapack\|modpack] [--version <版本>] [--loader <加载器>] [--limit N] [--offset N]` | 在 Modrinth 搜索：一次查询、一页结果 |
| `mcli add <项目> [--instance <名称>] [--version <版本>] [--loader <加载器>] [--type <类型>] [--dry-run]` | 把项目的匹配文件装进实例；`modpack` 会从项目自身识别，并按整个整合包安装（文件清单、覆盖文件、加载器） |
| `mcli mods [<名称>]` | 列出实例的 `mods/`、`resourcepacks/`、`shaderpacks/`、`datapacks/` |

## Java 与配置

| 命令 | 作用 |
| --- | --- |
| `mcli java [list \| install <major> [--force] \| which <version>]` | 查看或安装 Java 运行时 |
| `mcli config [get\|set\|unset\|list\|path]` | 读取或修改 `mcli.toml` 中的设置 |
| `mcli version`, `mcli help [command]` | 版本与帮助 |