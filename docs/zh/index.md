---
layout: home

hero:
  name: mcli
  text: "命令行里的 Minecraft: Java 版"
  tagline: "一个 Go 二进制，零第三方依赖。安装、启动、修复、开服 —— 用真实的微软账号，以及在官方主机慢的时候真的能用的镜像。"
  actions:
    - theme: brand
      text: 快速上手
      link: /zh/guide
    - theme: alt
      text: 命令参考
      link: /zh/commands
    - theme: alt
      text: GitHub
      link: https://github.com/Kailoinf/MCL-CLI

features:
  - title: 是启动器，不是壳
    details: "自己解析版本清单、算出确切需要的文件（客户端、库、natives、资源），带重试和卡死看门狗的下载，解压 natives，并自行拼出 java 命令行。"
  - title: 实例
    details: "具名实例各有自己的游戏目录、内存和参数。可以直接接管别的启动器（PCL、HMCL、官方启动器）已经装好的实例，不必重下一整个 GB。"
  - title: 多账号
    details: "微软登录走浏览器流程或设备码流程。刷新令牌与会话令牌分别放在两个独立加密文件里，`account list` 能看清谁登录着。"
  - title: 体检与修复
    details: "`mcli doctor` 说出缺了什么、坏了什么，有问题就以非零码退出；`mcli repair` 只补这一份清单，不多碰任何东西。"
  - title: 服务端
    details: "`mcli server` 安装服务端 jar、写出 eula.txt 和 server.properties，前台或后台运行，并能接上正在跑的控制台。"
  - title: 模组与资源包
    details: "`mcli search` 查询 Modrinth —— 一次查询、一页结果 —— `mcli add` 把匹配的文件按 SHA-1 校验后放进实例的 mods/、resourcepacks/ 或 shaderpacks/；整合包同样走 `add`。往原版实例装模组时，会告诉你该用加载器，而不是悄悄塞进一个加载不了的 jar。"
  - title: Fabric
    details: "加载器是版本上的一个选项：`create` 和 `instance set` 上的 `--loader fabric` 会取回 Fabric 发布的 profile，于是带模组的实例能跑起来 —— 包括别的启动器建好的那些。"
---

## 它长什么样

```sh
mcli login                                # 微软账号，浏览器流程
mcli create vanilla --version latest       # 用当前正式版建一个实例
mcli launch vanilla                        # 缺什么装什么，然后启动
mcli doctor --all                          # 有没有缺失或损坏？
mcli server create survival --version latest --eula
mcli server start survival --detach
mcli server attach survival                # 接上它的控制台，但不停止它
mcli create modded --version latest --loader fabric   # 或者 --loader vanilla
```

一切都是参数，没有向导。完整走查见[指南](/zh/guide)，参考见[命令](/zh/commands)，
内部实现见[架构](/zh/architecture)与[设计决策](/zh/design-decisions)。
`docs/status.md` —— [点这里](/zh/status) —— 是项目的实时状态：什么用真实命令验证过、
还剩什么、以及一路踩到的坑。