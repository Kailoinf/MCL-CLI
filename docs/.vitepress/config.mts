import { defineConfig } from "vitepress";

// The documentation site. `docs/status.md` is published unchanged: it is the live
// state of the project, meant to be read by people as well as by agents.
export default defineConfig({
  title: "mcli",
  description:
    "A command line Minecraft: Java Edition launcher: installs, launches, repairs and serves, with zero dependencies.",
  cleanUrls: true,
  srcExclude: ["**/README.md"],
  themeConfig: {
    nav: [
      { text: "Guide", link: "/guide" },
      { text: "Commands", link: "/commands" },
      { text: "Architecture", link: "/architecture" },
      { text: "Design", link: "/design-decisions" },
      { text: "Status", link: "/status" },
      { text: "Skill", link: "https://github.com/Kailoinf/MCL-CLI/blob/main/skill/SKILL.md" },
    ],
    sidebar: [
      {
        text: "Start here",
        items: [
          { text: "Guide", link: "/guide" },
          { text: "Commands", link: "/commands" },
        ],
      },
      {
        text: "Inside",
        items: [
          { text: "Architecture", link: "/architecture" },
          { text: "Design decisions", link: "/design-decisions" },
        ],
      },
      {
        text: "Project",
        items: [
          { text: "Live status", link: "/status" },
          {
            text: "Working on mcli (skill)",
            link: "https://github.com/Kailoinf/MCL-CLI/blob/main/skill/SKILL.md",
          },
        ],
      },
    ],
    outline: { level: [2, 3] },
    search: { provider: "local" },
    editLink: {
      pattern: "https://github.com/Kailoinf/MCL-CLI/edit/main/docs/:path",
      text: "Edit this page",
    },
    footer: {
      message: "Released under the MIT license",
      copyright: "Kailo",
    },
  },
  locales: {
    root: { label: "English", lang: "en" },
    zh: {
      label: "中文",
      lang: "zh-CN",
      link: "/zh/",
      themeConfig: {
        nav: [
          { text: "指南", link: "/zh/guide" },
          { text: "命令", link: "/zh/commands" },
          { text: "架构", link: "/zh/architecture" },
          { text: "状态", link: "/zh/status" },
        ],
        sidebar: [
          {
            text: "从这里开始",
            items: [
              { text: "首页", link: "/zh/" },
              { text: "指南", link: "/zh/guide" },
              { text: "命令参考", link: "/zh/commands" },
            ],
          },
          {
            text: "内部",
            items: [
              { text: "架构", link: "/zh/architecture" },
              { text: "设计决策", link: "/zh/design-decisions" },
            ],
          },
          {
            text: "项目",
            items: [
              { text: "实时状态", link: "/zh/status" },
              {
                text: "开发手册（skill）",
                link: "https://github.com/Kailoinf/MCL-CLI/blob/main/skill/SKILL.md",
              },
            ],
          },
        ],
      },
    },
  },
});
