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
| `mcli create <name> --version <v> [--fabric] [--loader <v>] [--java <path>] [--xmx 4G] [--xms 1G] [--jvm-arg …] [--game-arg …] [--force]` | 创建一个实例，可选地基于 Fabric loader |
| `mcli launch <name>\|<version>\|--last [--version <v>] [--java <path>] [--xmx] [--xms] [--jvm-arg] [--game-arg] [--width N] [--height N] [--server host[:port]] [--fullscreen] [--demo] [--dry-run] [--wait] [--account <a>]` | 按需安装并启动 |
| `mcli instance [list \| show <name> \| set <name> … \| copy <src> <dst> [--with-files] \| rename <old> <new> \| scan [--all] \| import <version> [--as <name>]]` | 查看和修改实例，接管已有的安装 |
| `mcli delete <name> [--keep-files] [--yes]` | 删除一个实例 |
| `mcli doctor [<name>] [--all] [--deep]` | 健康检查；发现问题时以 1 退出 |
| `mcli repair <name> [--deep] [--force] [--dry-run]` | 只抓取缺失或损坏的文件 |
| `mcli refresh [--versions] [--all]` | 强制刷新缓存的元数据 |

## 服务器

| 命令 | 作用 |
| --- | --- |
| `mcli server create <name> --version <v> [--java <path>] [--xmx 2G] [--xms 512M] [--port 25565] [--dir <path>] [--jvm-arg …] [--eula]` | 注册一个服务器 |
| `mcli server [list \| show <name> \| set <name> … \| delete <name> [--keep-files]]` | 查看和修改服务器 |
| `mcli server start <name> [--dry-run] [--detach] [--eula] [--port N]` | 按需抓取 jar 并启动 |
| `mcli server attach <name> [--lines N]` | 跟随后台服务器的控制台 |
| `mcli server cmd <name> <command…>` | 发送一条命令 |
| `mcli server stop <name> [--force]` | 发送 SIGINT，存档会保存 |

## Fabric

| 命令 | 作用 |
| --- | --- |
| `mcli fabric list <game> [--limit N]` | 某个游戏版本已发布的加载器构建 |
| `mcli fabric install <game> [<loader>] [--loader <v>]` | 安装一个加载器 profile |

## Java 与配置

| 命令 | 作用 |
| --- | --- |
| `mcli java [list \| install <major> [--force] \| which <version>]` | 查看或安装 Java 运行时 |
| `mcli config [get\|set\|unset\|list\|path]` | 读取或修改 `mcli.toml` 中的设置 |
| `mcli version`, `mcli help [command]` | 版本与帮助 |