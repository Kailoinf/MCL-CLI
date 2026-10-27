# 指南

## 安装

构建单个二进制（Go 1.26 或更新版本，仅使用标准库）：

```sh
git clone https://github.com/Kailoinf/MCL-CLI
cd MCL-CLI
go build -o mcli .
```

把它放到 `PATH` 上的某个位置。没有运行时依赖：不捆绑 Java，不需要 Python，也不需要 node。如果某个版本需要的 Java 运行时缺失，mcli 会自行安装。

## 首次运行

```sh
mcli login                       # opens the Microsoft page in a browser
mcli create vanilla --version latest
mcli launch vanilla
```

`launch` 会安装该版本需要的东西，然后启动游戏。游戏的输出写入 `<mcli home>/logs/game-<timestamp>.log`；`mcli doctor` 会打印最新的那一份。

## 文件都在哪里

```
~/.mcl-cli/     mcli.toml (config) · state.toml (identity, no secrets)
                refresh.toml (long-lived tokens) · session.toml (short-lived)
                last.toml (previous launch) · jdk/ · cache/ · logs/
~/.minecraft/   libraries/ · assets/ · versions/<version>/ · versions/<instance>/
```

`MCLI_HOME` 和 `MCLI_MINECRAFT` 可以覆盖这两个目录，靠它们才能为测试保留第二套互相隔离的配置。`mcli config path` 会打印这两个目录。

## 账号

可以同时登录多个账号。重装程序**不会**动 `~/.mcl-cli` 目录。如果那里的文件是很早以前的布局（比如 `refresh.toml` 里出现
本版本不认识的键），mcli 会说出它跳过了哪些键、并告知已存的登录用不了；它绝不会猜着改写旧文件。重新
`mcli login` 一次（会重写该文件）或直接删掉 `~/.mcl-cli/refresh.toml` 即可。

`mcli account list` 列出它们，`use` 切换默认账号，`launch --account <name>` 在单次运行中覆盖默认账号，`--no-switch` 能在不改变默认账号的前提下添加一个账号。凭据用从本机派生出的密钥加密，因此这两个 token 文件无法在别处解密 —— 而 `state.toml` 始终保持可移植。

## 实例

实例是一个具名目录，拥有自己的存档、选项和设置；体积大的共享部分（libraries、assets、版本文件）只在 `~/.minecraft` 中保存一份。`mcli instance set` 修改内存、Java 和启动参数；`mcli instance copy` 克隆一个实例；`mcli instance scan` 列出磁盘上的每一个版本目录，包括其他启动器创建的目录，`mcli instance import` 则原地接管其中一个 —— 文件和存档都留在原处。

`mcli list` 列的是 **mcli 已登记的实例**，不是游戏目录下的全部内容：把 `minecraft_dir` 指向一个装满东西的
`.minecraft`（官方启动器、PCL、HMCL 装的）本身不会登记任何东西，所以在一个完整安装旁边看到「还没有实例」
是正常的。列表发现未接管的版本时会明确说出来，接管只要一条命令：

```sh
mcli instance scan                    # 看有哪些、哪些已登记
mcli instance import 1.21.4           # 原地接管（--as <名称> 可改名）
```

切换目录时（`mcli config set minecraft_dir <路径>`）会顺便把新目录扫一遍，并告诉你还有几个版本没接管，
所以切完不会让你猜「怎么什么都没了」。

## 服务器

```sh
mcli server create survival --version latest --port 25566 --xmx 4G
mcli server start survival              # attached: the console is yours, Ctrl+C saves and stops
mcli server start survival --detach     # background, pid recorded
mcli server attach survival             # follow output and type commands; Ctrl+C only leaves
mcli server cmd survival say hello      # one command, no session
mcli server stop survival               # 用服务器自己的 stop，存档会保存
```

服务器位于 `<minecraft_dir>/servers/<name>`。`eula.txt` 和 `server.properties` 只创建一次，之后绝不覆盖；Minecraft EULA 的 URL 会被打印出来，必须接受（`--eula` 跳过的是询问，不是这条提示）。

## Fabric

```sh
mcli fabric list latest                                      # 查看某个游戏版本可用的加载器
mcli create modded --version latest --loader fabric          # 最新稳定加载器
mcli create pinned --version latest --loader fabric --loader-version 0.16.9
mcli instance set modded --loader vanilla                     # 也能切回原版
```

模组加载器是**版本上的一个选项**，不是单独一条命令：`create` 与 `instance set` 上的
`--loader vanilla|fabric`，需要固定某个构建时再加 `--loader-version`。取值超出这个选择
范围会直接报错并列出可选值，不会悄悄退回默认。Fabric 发布的 profile 会写入 mcli 自己的
`versions/` 目录，因此之后离线也能用，并且可以像其他任何版本一样启动、修复和诊断。

## CurseForge 需要你自己的 API key

Modrinth 什么都不需要；CurseForge 需要属于使用者本人的 API key，所以 mcli 不会自带一个：

```sh
mcli config set curseforge_api_key <你的 key>   # 保存在 mcli.toml
mcli config get curseforge_api_key              # 只报告长度，不显示值
mcli search jei --source curseforge
mcli add jei --instance modern --source curseforge
```

这个 key 以 `x-api-key` 发送，永不打印、也永不写进日志。没有 key 时，带 `--source curseforge`
的 `search`/`add` 会停下并告诉你执行的正是上面那条命令；被 CurseForge 拒绝的 key 会明确说
「被拒绝」，而不是伪装成网络故障。

## 模组与资源

mcli 用 Modrinth 作为来源 —— 它不需要 API key，接口也小到可以如实包一层。

```sh
mcli search sodium --version 1.21.4 --loader fabric    # 一次查询、一页结果
mcli search faithful --type resourcepack --version 1.21.4
mcli add sodium --instance modded                      # 下载匹配的那个文件
mcli add faithful --instance modded --dry-run          # 先看会放到哪
mcli mods modded                                       # 看看已经装了什么
```

* 搜索故意做薄：一次查询、一页结果（翻页用 `--limit`、`--offset`），只显示要装的
  slug、类型、下载量、作者和标题；没有需要长期维护的浏览界面。
* `add` 会挑出与实例的游戏版本和加载器匹配的最新版本，按 API 给的 SHA-1 下载它的
  主文件，再按项目类型放进正确目录：`mods/`、`resourcepacks/`、`shaderpacks/`、
  `datapacks/`。
* 模组需要加载器。往原版实例里装模组会被拒绝，并给出解决办法（`--loader fabric`），
  而不是把 Fabric 的 jar 丢进一个根本加载不了它的游戏；你确定要这么做时可以用
  `--loader` 覆盖。
* 必需的前置依赖会被打印出来，但不会自动安装：启动器悄悄多拉三个项目，只会
  留下一堆没人能复核的结果。

整合包走的也是 `add`：项目类型从项目本身读出来，所以 `.mrpack` 就按整合包安装。

```sh
mcli add fabulously-optimized --instance modded --dry-run   # 看这个整合包要什么
mcli add fabulously-optimized --instance modded             # 装上它
```

整合包就是「一堆文件 + 一个加载器 + 一个游戏版本」，装它基本是把文件抄到别人指定的位置 ——
启动器的职责是把不合适的地方说出来：

* 整合包自己的游戏版本会跟实例比对。把 1.21.4 的整合包装进 1.20.1 的实例会被拒绝，并给出
  应该执行的 `mcli create` 命令：装上去根本起不来，替你猜只会更糟。
* 整合包要求的加载器，在实例没有加载器时会自动装上；如果实例已有同一加载器的另一个构建，
  mcli 会说明并继续 —— 加载器版本差异通常没问题，但作者测试过的是它自己指定的那个。
* 每个文件都按整合包给出的 SHA-1 校验；会落到实例目录之外的路径一律拒绝。整合包是来自
  互联网的数据，不是「随便往哪写」的指令。
* 整合包的 `overrides/` 会被拷进去，但绝不覆盖已存在的文件：手动调过的配置或存档不是整合包
  可以覆盖的东西。被保留的文件会报告出来。

## 换版本（更新实例）

```sh
mcli update modded                       # 更新到最新正式版，保留它的加载器
mcli update modded --version 1.21.4      # 或者指定一个版本
mcli update modded --dry-run             # 只告诉你它会取什么
```

除游戏版本以外的一切都不动：游戏目录、内存、参数、模组、存档都在原地。带加载器的实例会
继续用同一个加载器 —— 新版本会做成对应的 profile —— 而旧版本的文件不会删，因为很可能还有
别的实例在用它。已经是最新版本时，它就只做一次完整性检查。

### 更新整合包

```sh
mcli add fabulously-optimized --instance modded --pack-version 15.0.0-beta.1   # 指定某个整合包版本
mcli update modded --pack --dry-run      # 先看这次会加什么、收回什么
mcli update modded --pack                # 换到适合它的最新版本
```

整合包更新就是两份清单：新版本要的文件，以及旧版本装过而新版本不再要的文件。第二份清单正是
安装时要记下整合包文件的原因 —— 每个文件都连同**它声明的摘要**一起记录。mcli 拿这份记录比对，
而不是和目录列表比对，所以你手动放进去的文件永远不会被删掉；而且只有当摘要仍然一致时才会删除，
所以你替换过的那个文件会被保留并报告出来。

## 支持的平台

mcli 是一个零依赖的 Go 二进制，下面这些平台都能编译，也都是按普通 Go 二进制开发的。
在仓库根目录跑 `./scripts/check-cross.sh` 会把它们全部构建一遍，某个目标编译不过就按 bug 处理。

| | Linux | Windows | macOS | 说明 |
| --- | --- | --- | --- | --- |
| 安装、启动、实例、账号 | 可以 | 可以 | 可以 | 核心能力 |
| 模组、资源包、整合包 | 可以 | 可以 | 可以 | Modrinth 不需要 key |
| 自动计算堆大小 | 可以 | 可以 | 可以 | Windows 问 kernel32，macOS 取物理内存一半 |
| 服务端（前台与后台） | 可以 | 可以 | 可以 | 后台输出写到 `logs/console.log` |
| `server attach` / `server cmd` | 可以 | **不可以** | 可以 | 需要给服务端输入用的 FIFO |
| 通过服务器控制台 `server stop` | 可以 | **不可以** | 可以 | Windows 没有 FIFO：`--force` 直接结束，存档保留在最后一次自动保存 |
| `doctor` 的可用空间一行 | 可以 | 可以 | 可以 | Windows 问 kernel32 |

缺什么，mcli 就会直说并给出替代做法，而不是抛一个看不懂的底层错误：Windows 上 `server attach`
会告诉你去前台运行服务端，优雅停止会告诉你要加 `--force`。

哪些是**真跑过**而不只是编译过的，写在[状态页](/status)里：除 Windows 与 macOS 外都在 Linux 上
验证过，Windows/macOS 是编译检查加上那些在任何平台都能跑的测试（比如 Windows 的 classpath 分隔符
与引号处理）。
